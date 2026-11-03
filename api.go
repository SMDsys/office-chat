package main

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

type App struct {
	store   *Store
	hub     *Hub
	log     *log.Logger
	limiter *limiter
	version string
	started time.Time
	secure  bool
}

type authedHandler func(w http.ResponseWriter, r *http.Request, u *User)

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func readJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		if errors.Is(err, io.EOF) {
			return errors.New("пустой запрос")
		}
		return errors.New("некорректный запрос")
	}
	return nil
}

func (a *App) auth(h authedHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !sameOrigin(r) {
			writeErr(w, http.StatusForbidden, "запрос отклонён")
			return
		}
		u := a.store.SessionUser(sessionToken(r))
		if u == nil {
			writeErr(w, http.StatusUnauthorized, "нужно войти в чат")
			return
		}
		h(w, r, u)
	}
}

func (a *App) public(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !sameOrigin(r) {
			writeErr(w, http.StatusForbidden, "запрос отклонён")
			return
		}
		h(w, r)
	}
}

// ---------- Состояние и вход ----------

func (a *App) handleState(w http.ResponseWriter, r *http.Request) {
	users := a.store.Users()
	writeJSON(w, 200, map[string]any{
		"hasUsers": len(users) > 0,
		"version":  a.version,
	})
}

func (a *App) handleRegister(w http.ResponseWriter, r *http.Request) {
	if !a.limiter.Allow("reg:"+clientIP(r), 6, 10*time.Minute) {
		writeErr(w, http.StatusTooManyRequests, "слишком много попыток, попробуйте позже")
		return
	}
	var req struct {
		Login    string `json:"login"`
		Name     string `json:"name"`
		Password string `json:"password"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	req.Login = strings.ToLower(strings.TrimSpace(req.Login))
	req.Name = strings.TrimSpace(req.Name)
	if runeLen(req.Password) < 6 {
		writeErr(w, 400, errPassInvalid.Error())
		return
	}
	if runeLen(req.Password) > 200 {
		writeErr(w, 400, "слишком длинный пароль")
		return
	}
	salt := newSalt()
	hash, err := hashPassword(req.Password, salt)
	if err != nil {
		writeErr(w, 500, "внутренняя ошибка")
		return
	}
	u, err := a.store.CreateUser(req.Login, req.Name, hash, salt)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	sess, err := a.store.CreateSession(u.ID)
	if err != nil {
		writeErr(w, 500, "внутренняя ошибка")
		return
	}
	a.setSessionCookie(w, sess.Token)
	a.log.Printf("новый пользователь: %s (%s)", u.Name, u.Login)
	a.hub.Broadcast(Event{Type: "user.new", Data: u.Public(true)})
	writeJSON(w, 200, map[string]any{"user": u.Public(true)})
}

func (a *App) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Login    string `json:"login"`
		Password string `json:"password"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	key := "login:" + clientIP(r) + ":" + strings.ToLower(req.Login)
	if !a.limiter.Allow(key, 10, 5*time.Minute) {
		writeErr(w, http.StatusTooManyRequests, "слишком много попыток входа, подождите 5 минут")
		return
	}
	u := a.store.UserByLogin(req.Login)
	if u == nil || !checkPassword(req.Password, u.Salt, u.PassHash) {
		writeErr(w, 401, "неверный логин или пароль")
		return
	}
	if u.Blocked {
		writeErr(w, 403, "учётная запись заблокирована")
		return
	}
	a.limiter.Reset(key)
	sess, err := a.store.CreateSession(u.ID)
	if err != nil {
		writeErr(w, 500, "внутренняя ошибка")
		return
	}
	a.setSessionCookie(w, sess.Token)
	a.store.TouchUser(u.ID)
	writeJSON(w, 200, map[string]any{"user": u.Public(a.hub.IsOnline(u.ID))})
}

func (a *App) handleLogout(w http.ResponseWriter, r *http.Request, u *User) {
	a.store.DeleteSession(sessionToken(r))
	clearSessionCookie(w)
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (a *App) handleMe(w http.ResponseWriter, r *http.Request, u *User) {
	writeJSON(w, 200, map[string]any{"user": u.Public(a.hub.IsOnline(u.ID))})
}

func (a *App) handleUpdateMe(w http.ResponseWriter, r *http.Request, u *User) {
	var req struct {
		Name  string `json:"name"`
		Color string `json:"color"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	name := strings.TrimSpace(req.Name)
	if name != "" && !validName(name) {
		writeErr(w, 400, errNameInvalid.Error())
		return
	}
	color := strings.TrimSpace(req.Color)
	if color != "" && !validColor(color) {
		writeErr(w, 400, "недопустимый цвет")
		return
	}
	updated, ok := a.store.UpdateUser(u.ID, func(x *User) {
		if name != "" {
			x.Name = name
		}
		if color != "" {
			x.Color = color
		}
	})
	if !ok {
		writeErr(w, 404, "пользователь не найден")
		return
	}
	a.hub.Broadcast(Event{Type: "user.update", Data: updated.Public(a.hub.IsOnline(u.ID))})
	writeJSON(w, 200, map[string]any{"user": updated.Public(a.hub.IsOnline(u.ID))})
}

func (a *App) handleChangePassword(w http.ResponseWriter, r *http.Request, u *User) {
	var req struct {
		Old string `json:"old"`
		New string `json:"new"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	full := a.store.User(u.ID)
	if full == nil || !checkPassword(req.Old, full.Salt, full.PassHash) {
		writeErr(w, 400, "текущий пароль неверен")
		return
	}
	if runeLen(req.New) < 6 {
		writeErr(w, 400, errPassInvalid.Error())
		return
	}
	salt := newSalt()
	hash, err := hashPassword(req.New, salt)
	if err != nil {
		writeErr(w, 500, "внутренняя ошибка")
		return
	}
	_, _ = a.store.UpdateUser(u.ID, func(x *User) {
		x.Salt = salt
		x.PassHash = hash
	})
	writeJSON(w, 200, map[string]any{"ok": true})
}

func validColor(c string) bool {
	for _, x := range userColors {
		if x == c {
			return true
		}
	}
	return false
}

// ---------- Загрузка состояния ----------

type chatView struct {
	Channel
	LastMessage *Message    `json:"lastMessage,omitempty"`
	Unread      int         `json:"unread"`
	DMUser      *PublicUser `json:"dmUser,omitempty"`
}

func (a *App) handleBootstrap(w http.ResponseWriter, r *http.Request, u *User) {
	users := a.store.Users()
	pub := make([]PublicUser, 0, len(users))
	byID := make(map[string]*User, len(users))
	for _, x := range users {
		byID[x.ID] = x
		pub = append(pub, x.Public(a.hub.IsOnline(x.ID)))
	}
	channels := a.store.ChannelsFor(u.ID)
	chats := make([]chatView, 0, len(channels))
	for _, c := range channels {
		v := chatView{
			Channel:     *c,
			LastMessage: a.store.LastMessage(c.ID),
			Unread:      a.store.Unread(u.ID, c.ID),
		}
		if c.IsDM {
			for _, m := range c.Members {
				if m != u.ID {
					if other := byID[m]; other != nil {
						pu := other.Public(a.hub.IsOnline(m))
						v.DMUser = &pu
					}
				}
			}
		}
		chats = append(chats, v)
	}
	writeJSON(w, 200, map[string]any{
		"me":         u.Public(true),
		"users":      pub,
		"chats":      chats,
		"online":     a.hub.OnlineUsers(),
		"serverTime": time.Now(),
		"version":    a.version,
	})
}

// ---------- Сообщения ----------

func (a *App) chatAllow(chatID string) func(string) bool {
	ch := a.store.Channel(chatID)
	if ch == nil || !ch.IsDM {
		return nil
	}
	members := ch.Members
	return func(uid string) bool { return contains(members, uid) }
}

func (a *App) handleMessages(w http.ResponseWriter, r *http.Request, u *User) {
	chatID := r.URL.Query().Get("chat")
	if chatID == "" || !a.store.CanAccess(chatID, u.ID) {
		writeErr(w, 403, "нет доступа к этому чату")
		return
	}
	var before time.Time
	if s := r.URL.Query().Get("before"); s != "" {
		if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
			before = t
		}
	}
	limit := 50
	if s := r.URL.Query().Get("limit"); s != "" {
		fmt.Sscanf(s, "%d", &limit)
	}
	msgs, hasMore := a.store.Messages(chatID, before, limit)
	writeJSON(w, 200, map[string]any{"messages": msgs, "hasMore": hasMore})
}

func (a *App) handleSendMessage(w http.ResponseWriter, r *http.Request, u *User) {
	var req struct {
		ChatID  string   `json:"chatId"`
		Text    string   `json:"text"`
		ReplyTo string   `json:"replyTo"`
		Files   []string `json:"files"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if !a.store.CanAccess(req.ChatID, u.ID) {
		writeErr(w, 403, "нет доступа к этому чату")
		return
	}
	text := cleanText(req.Text)
	if runeLen(text) > 4000 {
		writeErr(w, 400, "сообщение длиннее 4000 символов")
		return
	}
	files := make([]Attachment, 0, len(req.Files))
	for _, id := range req.Files {
		meta := a.store.Upload(id)
		if meta == nil || meta.UserID != u.ID {
			writeErr(w, 400, "файл не найден, загрузите его заново")
			return
		}
		files = append(files, meta.Attachment)
	}
	if text == "" && len(files) == 0 {
		writeErr(w, 400, "сообщение пустое")
		return
	}
	if len(files) > 10 {
		writeErr(w, 400, "не больше 10 файлов в одном сообщении")
		return
	}
	a.store.BindUploads(req.Files, req.ChatID)
	if req.ReplyTo != "" {
		parent, chatID := a.store.Message(req.ReplyTo)
		if parent == nil || chatID != req.ChatID || parent.Deleted {
			req.ReplyTo = ""
		}
	}
	msg := &Message{
		ID:        randomID(),
		ChatID:    req.ChatID,
		UserID:    u.ID,
		Text:      text,
		CreatedAt: time.Now(),
		ReplyTo:   req.ReplyTo,
		Files:     files,
		Reactions: map[string][]string{},
	}
	a.store.AddMessage(msg)
	a.store.MarkRead(u.ID, req.ChatID, msg.CreatedAt)
	a.hub.Publish(Event{Type: "message", Data: msg}, a.chatAllow(req.ChatID))
	writeJSON(w, 200, map[string]any{"message": msg})
}

func (a *App) handleEditMessage(w http.ResponseWriter, r *http.Request, u *User) {
	id := r.PathValue("id")
	msg, chatID := a.store.Message(id)
	if msg == nil {
		writeErr(w, 404, "сообщение не найдено")
		return
	}
	if !a.store.CanAccess(chatID, u.ID) {
		writeErr(w, 403, "нет доступа к этому чату")
		return
	}
	if msg.UserID != u.ID && !u.IsAdmin {
		writeErr(w, 403, "можно редактировать только свои сообщения")
		return
	}
	if msg.Deleted {
		writeErr(w, 400, "сообщение удалено")
		return
	}
	var req struct {
		Text string `json:"text"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	text := cleanText(req.Text)
	if text == "" {
		writeErr(w, 400, "текст не может быть пустым")
		return
	}
	if runeLen(text) > 4000 {
		writeErr(w, 400, "сообщение длиннее 4000 символов")
		return
	}
	now := time.Now()
	updated, _, _ := a.store.UpdateMessage(id, func(m *Message) {
		m.Text = text
		m.EditedAt = &now
	})
	a.hub.Publish(Event{Type: "message.update", Data: updated}, a.chatAllow(chatID))
	writeJSON(w, 200, map[string]any{"message": updated})
}

func (a *App) handleDeleteMessage(w http.ResponseWriter, r *http.Request, u *User) {
	id := r.PathValue("id")
	msg, chatID := a.store.Message(id)
	if msg == nil {
		writeErr(w, 404, "сообщение не найдено")
		return
	}
	if !a.store.CanAccess(chatID, u.ID) {
		writeErr(w, 403, "нет доступа к этому чату")
		return
	}
	if msg.UserID != u.ID && !u.IsAdmin {
		writeErr(w, 403, "можно удалять только свои сообщения")
		return
	}
	updated, _, _ := a.store.UpdateMessage(id, func(m *Message) {
		m.Deleted = true
		m.Text = ""
		m.Files = nil
		m.Reactions = map[string][]string{}
	})
	a.hub.Publish(Event{Type: "message.update", Data: updated}, a.chatAllow(chatID))
	writeJSON(w, 200, map[string]any{"message": updated})
}

var allowedReactions = map[string]bool{
	"👍": true, "❤️": true, "😂": true, "🎉": true, "👀": true, "✅": true,
}

func (a *App) handleReact(w http.ResponseWriter, r *http.Request, u *User) {
	id := r.PathValue("id")
	msg, chatID := a.store.Message(id)
	if msg == nil || msg.Deleted {
		writeErr(w, 404, "сообщение не найдено")
		return
	}
	if !a.store.CanAccess(chatID, u.ID) {
		writeErr(w, 403, "нет доступа")
		return
	}
	var req struct {
		Emoji string `json:"emoji"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if !allowedReactions[req.Emoji] {
		writeErr(w, 400, "недопустимая реакция")
		return
	}
	updated, _, _ := a.store.UpdateMessage(id, func(m *Message) {
		if m.Reactions == nil {
			m.Reactions = map[string][]string{}
		}
		list := m.Reactions[req.Emoji]
		found := false
		for i, x := range list {
			if x == u.ID {
				m.Reactions[req.Emoji] = append(list[:i], list[i+1:]...)
				found = true
				break
			}
		}
		if !found {
			m.Reactions[req.Emoji] = append(list, u.ID)
		}
		if len(m.Reactions[req.Emoji]) == 0 {
			delete(m.Reactions, req.Emoji)
		}
	})
	a.hub.Publish(Event{Type: "message.update", Data: updated}, a.chatAllow(chatID))
	writeJSON(w, 200, map[string]any{"message": updated})
}

func (a *App) handlePin(w http.ResponseWriter, r *http.Request, u *User) {
	id := r.PathValue("id")
	msg, chatID := a.store.Message(id)
	if msg == nil || msg.Deleted {
		writeErr(w, 404, "сообщение не найдено")
		return
	}
	if !a.store.CanAccess(chatID, u.ID) {
		writeErr(w, 403, "нет доступа")
		return
	}
	var req struct {
		Pinned bool `json:"pinned"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	updated, _, _ := a.store.UpdateMessage(id, func(m *Message) { m.Pinned = req.Pinned })
	a.hub.Publish(Event{Type: "message.update", Data: updated}, a.chatAllow(chatID))
	writeJSON(w, 200, map[string]any{"message": updated})
}

// ---------- Чаты ----------

func (a *App) handleCreateChannel(w http.ResponseWriter, r *http.Request, u *User) {
	var req struct {
		Name  string `json:"name"`
		Topic string `json:"topic"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	ch, err := a.store.CreateChannel(req.Name, req.Topic, u.ID)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	a.hub.Broadcast(Event{Type: "chat.new", Data: chatView{Channel: *ch}})
	writeJSON(w, 200, map[string]any{"chat": ch})
}

func (a *App) handleUpdateChannel(w http.ResponseWriter, r *http.Request, u *User) {
	id := r.PathValue("id")
	ch := a.store.Channel(id)
	if ch == nil || ch.IsDM {
		writeErr(w, 404, "канал не найден")
		return
	}
	if ch.CreatedBy != u.ID && !u.IsAdmin {
		writeErr(w, 403, "менять канал может только его создатель")
		return
	}
	var req struct {
		Name  string `json:"name"`
		Topic string `json:"topic"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	name := strings.TrimSpace(req.Name)
	topic := strings.TrimSpace(req.Topic)
	if runeLen(name) < 1 || runeLen(name) > 40 {
		writeErr(w, 400, "название канала: от 1 до 40 символов")
		return
	}
	if runeLen(topic) > 160 {
		writeErr(w, 400, "описание — до 160 символов")
		return
	}
	updated, _ := a.store.UpdateChannel(id, func(c *Channel) {
		c.Name = name
		c.Topic = topic
	})
	a.hub.Broadcast(Event{Type: "chat.update", Data: updated})
	writeJSON(w, 200, map[string]any{"chat": updated})
}

func (a *App) handleDeleteChannel(w http.ResponseWriter, r *http.Request, u *User) {
	id := r.PathValue("id")
	ch := a.store.Channel(id)
	if ch == nil || ch.IsDM {
		writeErr(w, 404, "канал не найден")
		return
	}
	if ch.CreatedBy != u.ID && !u.IsAdmin {
		writeErr(w, 403, "удалить канал может только его создатель")
		return
	}
	if err := a.store.DeleteChannel(id); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	a.hub.Broadcast(Event{Type: "chat.deleted", Data: map[string]string{"chatId": id}})
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (a *App) handleCreateDM(w http.ResponseWriter, r *http.Request, u *User) {
	var req struct {
		UserID string `json:"userId"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	ch, err := a.store.GetOrCreateDM(u.ID, req.UserID)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	view := chatView{Channel: *ch}
	if other := a.store.User(req.UserID); other != nil {
		pu := other.Public(a.hub.IsOnline(other.ID))
		view.DMUser = &pu
	}
	a.hub.Publish(Event{Type: "chat.new", Data: view}, a.chatAllow(ch.ID))
	writeJSON(w, 200, map[string]any{"chat": view})
}

func (a *App) handleRead(w http.ResponseWriter, r *http.Request, u *User) {
	var req struct {
		ChatID string    `json:"chatId"`
		At     time.Time `json:"at"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if !a.store.CanAccess(req.ChatID, u.ID) {
		writeErr(w, 403, "нет доступа")
		return
	}
	at := req.At
	if at.IsZero() || at.After(time.Now()) {
		at = time.Now()
	}
	a.store.MarkRead(u.ID, req.ChatID, at)
	a.hub.Publish(Event{Type: "read", Data: map[string]any{
		"chatId": req.ChatID,
		"userId": u.ID,
		"at":     at,
	}}, func(uid string) bool { return uid == u.ID })
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (a *App) handleReadAll(w http.ResponseWriter, r *http.Request, u *User) {
	at := time.Now()
	a.store.MarkAllRead(u.ID, at)
	a.hub.Publish(Event{Type: "read", Data: map[string]any{
		"chatId": "",
		"userId": u.ID,
		"at":     at,
	}}, func(uid string) bool { return uid == u.ID })
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (a *App) handleTyping(w http.ResponseWriter, r *http.Request, u *User) {
	var req struct {
		ChatID string `json:"chatId"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if !a.store.CanAccess(req.ChatID, u.ID) {
		writeErr(w, 403, "нет доступа")
		return
	}
	a.hub.Publish(Event{Type: "typing", Data: map[string]any{
		"chatId": req.ChatID,
		"userId": u.ID,
		"name":   u.Name,
	}}, func(uid string) bool { return uid != u.ID && a.store.CanAccess(req.ChatID, uid) })
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) handlePins(w http.ResponseWriter, r *http.Request, u *User) {
	chatID := r.URL.Query().Get("chat")
	if chatID == "" || !a.store.CanAccess(chatID, u.ID) {
		writeErr(w, 403, "нет доступа к этому чату")
		return
	}
	writeJSON(w, 200, map[string]any{"messages": a.store.Pinned(chatID)})
}

func (a *App) handleSearch(w http.ResponseWriter, r *http.Request, u *User) {
	q := r.URL.Query().Get("q")
	chatID := r.URL.Query().Get("chat")
	if runeLen(q) < 2 {
		writeJSON(w, 200, map[string]any{"messages": []*Message{}})
		return
	}
	msgs := a.store.Search(u.ID, q, chatID, 100)
	writeJSON(w, 200, map[string]any{"messages": msgs})
}

// ---------- Администратор ----------

func (a *App) requireAdmin(w http.ResponseWriter, u *User) bool {
	if !u.IsAdmin {
		writeErr(w, 403, "действие доступно только администратору")
		return false
	}
	return true
}

func (a *App) handleAdminReset(w http.ResponseWriter, r *http.Request, u *User) {
	if !a.requireAdmin(w, u) {
		return
	}
	id := r.PathValue("id")
	target := a.store.User(id)
	if target == nil {
		writeErr(w, 404, "пользователь не найден")
		return
	}
	password := generatePassword(10)
	salt := newSalt()
	hash, err := hashPassword(password, salt)
	if err != nil {
		writeErr(w, 500, "внутренняя ошибка")
		return
	}
	_, _ = a.store.UpdateUser(id, func(x *User) {
		x.Salt = salt
		x.PassHash = hash
	})
	a.store.DeleteUserSessions(id)
	a.log.Printf("админ %s сбросил пароль пользователю %s", u.Login, target.Login)
	writeJSON(w, 200, map[string]any{"password": password})
}

func (a *App) handleAdminBlock(w http.ResponseWriter, r *http.Request, u *User) {
	if !a.requireAdmin(w, u) {
		return
	}
	id := r.PathValue("id")
	if id == u.ID {
		writeErr(w, 400, "нельзя заблокировать себя")
		return
	}
	var req struct {
		Blocked bool `json:"blocked"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	target, ok := a.store.UpdateUser(id, func(x *User) { x.Blocked = req.Blocked })
	if !ok {
		writeErr(w, 404, "пользователь не найден")
		return
	}
	if req.Blocked {
		a.store.DeleteUserSessions(id)
	}
	a.hub.Broadcast(Event{Type: "user.update", Data: target.Public(false)})
	writeJSON(w, 200, map[string]any{"ok": true})
}

func generatePassword(n int) string {
	const alphabet = "abcdefghijkmnpqrstuvwxyz23456789"
	b := make([]byte, n)
	raw := make([]byte, n)
	_, _ = rand.Read(raw)
	for i := range b {
		b[i] = alphabet[int(raw[i])%len(alphabet)]
	}
	return string(b)
}
