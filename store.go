package main

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const generalChannelID = "general"

type chatData struct {
	Messages []*Message `json:"messages"`
}

// Store хранит все данные в памяти и периодически сбрасывает изменения на диск.
type Store struct {
	mu       sync.Mutex
	dir      string
	chatsDir string
	upDir    string

	users    map[string]*User
	byLogin  map[string]string
	channels map[string]*Channel
	chats    map[string]*chatData
	msgs     map[string]*Message
	msgChat  map[string]string
	sessions map[string]*Session
	uploads  map[string]*UploadMeta

	dirtyUsers    bool
	dirtyChannels bool
	dirtySessions bool
	dirtyUploads  bool
	dirtyChats    map[string]bool

	stop       chan struct{}
	done       chan struct{}
	log        *log.Logger
	lastBackup time.Time
}

func NewStore(dir string, logger *log.Logger) (*Store, error) {
	s := &Store{
		dir:        dir,
		chatsDir:   filepath.Join(dir, "chats"),
		upDir:      filepath.Join(dir, "uploads"),
		users:      map[string]*User{},
		byLogin:    map[string]string{},
		channels:   map[string]*Channel{},
		chats:      map[string]*chatData{},
		msgs:       map[string]*Message{},
		msgChat:    map[string]string{},
		sessions:   map[string]*Session{},
		uploads:    map[string]*UploadMeta{},
		dirtyChats: map[string]bool{},
		stop:       make(chan struct{}),
		done:       make(chan struct{}),
		log:        logger,
	}
	for _, d := range []string{dir, s.chatsDir, s.upDir, filepath.Join(dir, "backups"), filepath.Join(dir, "tmp")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, err
		}
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	s.ensureGeneral()
	s.migrateUploadChats()
	go s.saveLoop()
	return s, nil
}

func (s *Store) load() error {
	var users []*User
	if err := s.readJSONFile(filepath.Join(s.dir, "users.json"), &users); err != nil {
		return err
	}
	for _, u := range users {
		if u.Reads == nil {
			u.Reads = map[string]time.Time{}
		}
		s.users[u.ID] = u
		s.byLogin[strings.ToLower(u.Login)] = u.ID
	}
	var channels []*Channel
	if err := s.readJSONFile(filepath.Join(s.dir, "channels.json"), &channels); err != nil {
		return err
	}
	for _, c := range channels {
		s.channels[c.ID] = c
	}
	var sessions []*Session
	if err := s.readJSONFile(filepath.Join(s.dir, "sessions.json"), &sessions); err != nil {
		return err
	}
	now := time.Now()
	for _, sess := range sessions {
		if sess.ExpiresAt.After(now) {
			s.sessions[sess.Token] = sess
		} else {
			s.dirtySessions = true
		}
	}
	var uploads []*UploadMeta
	if err := s.readJSONFile(filepath.Join(s.dir, "uploads.json"), &uploads); err != nil {
		return err
	}
	for _, m := range uploads {
		s.uploads[m.ID] = m
	}

	files, err := filepath.Glob(filepath.Join(s.chatsDir, "*.json"))
	if err != nil {
		return err
	}
	for _, f := range files {
		id := strings.TrimSuffix(filepath.Base(f), ".json")
		var data chatData
		if err := s.readJSONFile(f, &data); err != nil {
			s.log.Printf("не удалось прочитать %s: %v (файл переименован)", f, err)
			_ = os.Rename(f, f+".broken")
			continue
		}
		for _, m := range data.Messages {
			if m == nil || m.ID == "" {
				continue
			}
			if m.Reactions == nil {
				m.Reactions = map[string][]string{}
			}
			s.msgs[m.ID] = m
			s.msgChat[m.ID] = id
		}
		s.chats[id] = &data
	}
	return nil
}

// readJSONFile читает файл, отсутствие файла не считается ошибкой.
func (s *Store) readJSONFile(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		return nil
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return nil
}

func (s *Store) ensureGeneral() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.channels[generalChannelID]; ok {
		return
	}
	c := &Channel{
		ID:        generalChannelID,
		Name:      "Общий",
		Topic:     "Общий канал — здесь пишут все",
		CreatedAt: time.Now(),
		CreatedBy: "system",
	}
	s.channels[c.ID] = c
	s.dirtyChannels = true
	if s.chats[c.ID] == nil {
		s.chats[c.ID] = &chatData{}
		s.dirtyChats[c.ID] = true
	}
}

// ---------- Сохранение ----------

func (s *Store) saveLoop() {
	defer close(s.done)
	t := time.NewTicker(350 * time.Millisecond)
	defer t.Stop()
	backupTick := time.NewTicker(6 * time.Hour)
	defer backupTick.Stop()
	cleanTick := time.NewTicker(30 * time.Minute)
	defer cleanTick.Stop()
	for {
		select {
		case <-t.C:
			s.flush()
		case <-backupTick.C:
			s.maybeBackup()
		case <-cleanTick.C:
			s.cleanup()
		case <-s.stop:
			s.flush()
			return
		}
	}
}

func (s *Store) Close() {
	close(s.stop)
	<-s.done
}

func (s *Store) flush() {
	s.mu.Lock()
	type fileData struct {
		path string
		data []byte
	}
	var files []fileData
	if s.dirtyUsers {
		if b, err := json.MarshalIndent(mapUsers(s.users), "", "  "); err == nil {
			files = append(files, fileData{filepath.Join(s.dir, "users.json"), b})
		} else {
			s.log.Printf("users.json: %v", err)
		}
		s.dirtyUsers = false
	}
	if s.dirtyChannels {
		if b, err := json.MarshalIndent(mapChannels(s.channels), "", "  "); err == nil {
			files = append(files, fileData{filepath.Join(s.dir, "channels.json"), b})
		} else {
			s.log.Printf("channels.json: %v", err)
		}
		s.dirtyChannels = false
	}
	if s.dirtySessions {
		if b, err := json.MarshalIndent(mapSessions(s.sessions), "", "  "); err == nil {
			files = append(files, fileData{filepath.Join(s.dir, "sessions.json"), b})
		}
		s.dirtySessions = false
	}
	if s.dirtyUploads {
		if b, err := json.MarshalIndent(mapUploads(s.uploads), "", "  "); err == nil {
			files = append(files, fileData{filepath.Join(s.dir, "uploads.json"), b})
		}
		s.dirtyUploads = false
	}
	for id := range s.dirtyChats {
		chat := s.chats[id]
		if chat == nil {
			continue
		}
		if b, err := json.Marshal(chat); err == nil {
			files = append(files, fileData{filepath.Join(s.chatsDir, id+".json"), b})
		} else {
			s.log.Printf("чат %s: %v", id, err)
		}
		delete(s.dirtyChats, id)
	}
	s.mu.Unlock()

	for _, f := range files {
		if err := writeFileAtomic(f.path, f.data); err != nil {
			s.log.Printf("запись %s: %v", f.path, err)
		}
	}
}

func writeFileAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func mapUsers(m map[string]*User) []*User {
	out := make([]*User, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

func mapChannels(m map[string]*Channel) []*Channel {
	out := make([]*Channel, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

func mapSessions(m map[string]*Session) []*Session {
	out := make([]*Session, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	return out
}

func mapUploads(m map[string]*UploadMeta) []*UploadMeta {
	out := make([]*UploadMeta, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	return out
}

// ---------- Пользователи ----------

var (
	errLoginTaken   = errors.New("такой логин уже занят")
	errLoginInvalid = errors.New("логин: 3–24 символа, латиница, цифры, точка, дефис или подчёркивание")
	errPassInvalid  = errors.New("пароль должен быть не короче 6 символов")
	errNameInvalid  = errors.New("укажите имя (до 60 символов)")
)

func validLogin(login string) bool {
	if len(login) < 3 || len(login) > 24 {
		return false
	}
	for _, r := range login {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

func (s *Store) CreateUser(login, name, passHash, salt string) (*User, error) {
	login = strings.ToLower(strings.TrimSpace(login))
	name = strings.TrimSpace(name)
	if !validLogin(login) {
		return nil, errLoginInvalid
	}
	if runeLen(name) < 1 || runeLen(name) > 60 {
		return nil, errNameInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.byLogin[login]; exists {
		return nil, errLoginTaken
	}
	u := &User{
		ID:        randomID(),
		Login:     login,
		Name:      name,
		PassHash:  passHash,
		Salt:      salt,
		Color:     colorFor(login),
		IsAdmin:   len(s.users) == 0,
		CreatedAt: time.Now(),
		LastSeen:  time.Now(),
		Reads:     map[string]time.Time{},
	}
	s.users[u.ID] = u
	s.byLogin[login] = u.ID
	s.dirtyUsers = true
	return u, nil
}

func (s *Store) User(id string) *User {
	s.mu.Lock()
	defer s.mu.Unlock()
	u := s.users[id]
	if u == nil {
		return nil
	}
	c := *u
	if u.Reads != nil {
		c.Reads = make(map[string]time.Time, len(u.Reads))
		for k, v := range u.Reads {
			c.Reads[k] = v
		}
	}
	return &c
}

func (s *Store) UserByLogin(login string) *User {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.byLogin[strings.ToLower(strings.TrimSpace(login))]
	if !ok {
		return nil
	}
	u := s.users[id]
	if u == nil {
		return nil
	}
	c := *u
	if u.Reads != nil {
		c.Reads = make(map[string]time.Time, len(u.Reads))
		for k, v := range u.Reads {
			c.Reads[k] = v
		}
	}
	return &c
}

func (s *Store) Users() []*User {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := mapUsers(s.users)
	cp := make([]*User, 0, len(out))
	for _, u := range out {
		c := *u
		cp = append(cp, &c)
	}
	return cp
}

func (s *Store) UpdateUser(id string, fn func(*User)) (*User, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u := s.users[id]
	if u == nil {
		return nil, false
	}
	fn(u)
	s.dirtyUsers = true
	c := *u
	return &c, true
}

func (s *Store) TouchUser(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if u := s.users[id]; u != nil {
		u.LastSeen = time.Now()
	}
}

func (s *Store) MarkRead(userID, chatID string, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u := s.users[userID]
	if u == nil {
		return
	}
	if u.Reads == nil {
		u.Reads = map[string]time.Time{}
	}
	if cur := u.Reads[chatID]; at.After(cur) {
		u.Reads[chatID] = at
		s.dirtyUsers = true
	}
}

// MarkAllRead отмечает прочитанными все чаты, доступные пользователю.
func (s *Store) MarkAllRead(userID string, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u := s.users[userID]
	if u == nil {
		return
	}
	if u.Reads == nil {
		u.Reads = map[string]time.Time{}
	}
	for _, c := range s.channels {
		if c.IsDM && !contains(c.Members, userID) {
			continue
		}
		if cur := u.Reads[c.ID]; at.After(cur) {
			u.Reads[c.ID] = at
			s.dirtyUsers = true
		}
	}
}

// ---------- Каналы ----------

func (s *Store) Channel(id string) *Channel {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.channels[id]
	if c == nil {
		return nil
	}
	cp := *c
	if c.Members != nil {
		cp.Members = append([]string(nil), c.Members...)
	}
	return &cp
}

func (s *Store) ChannelsFor(userID string) []*Channel {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Channel, 0, len(s.channels))
	for _, c := range s.channels {
		if (c.IsDM || c.Private) && !contains(c.Members, userID) {
			continue
		}
		cp := *c
		if c.Members != nil {
			cp.Members = append([]string(nil), c.Members...)
		}
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].IsDM != out[j].IsDM {
			return !out[i].IsDM
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out
}

func (s *Store) CreateChannel(name, topic, by string) (*Channel, error) {
	return s.CreateChannelWith(name, topic, by, false, nil)
}

// CreateChannelWith создаёт канал; для закрытого канала участники обязательны.
func (s *Store) CreateChannelWith(name, topic, by string, private bool, members []string) (*Channel, error) {
	name = strings.TrimSpace(name)
	topic = strings.TrimSpace(topic)
	if runeLen(name) < 1 || runeLen(name) > 40 {
		return nil, errors.New("название канала: от 1 до 40 символов")
	}
	if runeLen(topic) > 160 {
		return nil, errors.New("описание канала — до 160 символов")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.channels {
		if !c.IsDM && strings.EqualFold(c.Name, name) {
			return nil, errors.New("канал с таким названием уже есть")
		}
	}
	if private {
		members = append([]string{by}, members...)
		members = uniqueIDs(members)
	}
	c := &Channel{
		ID:        randomID(),
		Name:      name,
		Topic:     topic,
		Private:   private,
		CreatedBy: by,
		CreatedAt: time.Now(),
	}
	if private {
		c.Members = members
	}
	s.channels[c.ID] = c
	s.chats[c.ID] = &chatData{}
	s.dirtyChannels = true
	s.dirtyChats[c.ID] = true
	cp := *c
	return &cp, nil
}

func (s *Store) UpdateChannel(id string, fn func(*Channel)) (*Channel, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.channels[id]
	if c == nil {
		return nil, false
	}
	fn(c)
	s.dirtyChannels = true
	cp := *c
	return &cp, true
}

func (s *Store) DeleteChannel(id string) error {
	if id == generalChannelID {
		return errors.New("общий канал удалить нельзя")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.channels[id]; !ok {
		return errors.New("канал не найден")
	}
	delete(s.channels, id)
	s.dirtyChannels = true
	return nil
}

func dmID(a, b string) string {
	if a > b {
		a, b = b, a
	}
	return "dm_" + a + "_" + b
}

func (s *Store) GetOrCreateDM(a, b string) (*Channel, error) {
	if a == b {
		return nil, errors.New("нельзя начать переписку с собой")
	}
	id := dmID(a, b)
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.channels[id]; ok {
		cp := *c
		return &cp, nil
	}
	if s.users[a] == nil || s.users[b] == nil {
		return nil, errors.New("пользователь не найден")
	}
	c := &Channel{
		ID:        id,
		IsDM:      true,
		Members:   []string{a, b},
		CreatedAt: time.Now(),
		CreatedBy: a,
	}
	s.channels[id] = c
	s.chats[id] = &chatData{}
	s.dirtyChannels = true
	s.dirtyChats[id] = true
	cp := *c
	cp.Members = append([]string(nil), c.Members...)
	return &cp, nil
}

func (s *Store) CanAccess(chatID, userID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.channels[chatID]
	if c == nil {
		return false
	}
	if c.IsDM || c.Private {
		return contains(c.Members, userID)
	}
	return true
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// ---------- Сообщения ----------

func (s *Store) AddMessage(m *Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	chat := s.chats[m.ChatID]
	if chat == nil {
		chat = &chatData{}
		s.chats[m.ChatID] = chat
	}
	chat.Messages = append(chat.Messages, m)
	s.msgs[m.ID] = m
	s.msgChat[m.ID] = m.ChatID
	s.dirtyChats[m.ChatID] = true
}

func (s *Store) Message(id string) (*Message, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.msgs[id]
	if m == nil {
		return nil, ""
	}
	return m.Clone(), s.msgChat[id]
}

func (s *Store) UpdateMessage(id string, fn func(*Message)) (*Message, string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.msgs[id]
	if m == nil {
		return nil, "", false
	}
	fn(m)
	chatID := s.msgChat[id]
	s.dirtyChats[chatID] = true
	return m.Clone(), chatID, true
}

func (s *Store) Messages(chatID string, before time.Time, limit int) ([]*Message, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	chat := s.chats[chatID]
	if chat == nil {
		return []*Message{}, false
	}
	msgs := chat.Messages
	end := len(msgs)
	if !before.IsZero() {
		for end > 0 && !msgs[end-1].CreatedAt.Before(before) {
			end--
		}
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	start := end - limit
	if start < 0 {
		start = 0
	}
	out := make([]*Message, 0, end-start)
	for _, m := range msgs[start:end] {
		out = append(out, m.Clone())
	}
	return out, start > 0
}

func (s *Store) LastMessage(chatID string) *Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	chat := s.chats[chatID]
	if chat == nil {
		return nil
	}
	for i := len(chat.Messages) - 1; i >= 0; i-- {
		if !chat.Messages[i].Deleted {
			return chat.Messages[i].Clone()
		}
	}
	return nil
}

func (s *Store) Pinned(chatID string) []*Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	chat := s.chats[chatID]
	if chat == nil {
		return []*Message{}
	}
	out := []*Message{}
	for _, m := range chat.Messages {
		if m.Pinned && !m.Deleted {
			out = append(out, m.Clone())
		}
	}
	return out
}

func (s *Store) Unread(userID, chatID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	u := s.users[userID]
	chat := s.chats[chatID]
	if u == nil || chat == nil {
		return 0
	}
	read := u.Reads[chatID]
	if read.IsZero() {
		read = u.CreatedAt
	}
	n := 0
	for _, m := range chat.Messages {
		if m.UserID == userID || m.Deleted {
			continue
		}
		if m.CreatedAt.After(read) {
			n++
		}
	}
	if n > 999 {
		n = 999
	}
	return n
}

func (s *Store) Search(userID, q, chatID string, limit int) []*Message {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return []*Message{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	chatIDs := []string{}
	if chatID != "" {
		if c := s.channels[chatID]; c != nil && ((!c.IsDM && !c.Private) || contains(c.Members, userID)) {
			chatIDs = append(chatIDs, chatID)
		}
	} else {
		for id, c := range s.channels {
			if (!c.IsDM && !c.Private) || contains(c.Members, userID) {
				chatIDs = append(chatIDs, id)
			}
		}
	}
	out := []*Message{}
	all := []*Message{}
	for _, id := range chatIDs {
		if chat := s.chats[id]; chat != nil {
			all = append(all, chat.Messages...)
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].CreatedAt.After(all[j].CreatedAt) })
	for _, m := range all {
		if m.Deleted {
			continue
		}
		if strings.Contains(strings.ToLower(m.Text), q) {
			out = append(out, m.Clone())
			if len(out) >= limit {
				break
			}
		}
	}
	return out
}

// ---------- Сессии ----------

const sessionTTL = 30 * 24 * time.Hour

func (s *Store) CreateSession(userID string) (*Session, error) {
	tok := randomToken(32)
	sess := &Session{
		Token:     tok,
		UserID:    userID,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(sessionTTL),
	}
	s.mu.Lock()
	s.sessions[tok] = sess
	s.dirtySessions = true
	s.mu.Unlock()
	return sess, nil
}

func (s *Store) SessionUser(token string) *User {
	if token == "" {
		return nil
	}
	s.mu.Lock()
	sess := s.sessions[token]
	if sess == nil {
		s.mu.Unlock()
		return nil
	}
	if time.Now().After(sess.ExpiresAt) {
		delete(s.sessions, token)
		s.dirtySessions = true
		s.mu.Unlock()
		return nil
	}
	u := s.users[sess.UserID]
	if u == nil || u.Blocked {
		s.mu.Unlock()
		return nil
	}
	c := *u
	s.mu.Unlock()
	return &c
}

func (s *Store) DeleteSession(token string) {
	s.mu.Lock()
	delete(s.sessions, token)
	s.dirtySessions = true
	s.mu.Unlock()
}

// DeleteOtherSessions завершает все сеансы пользователя, кроме указанного.
func (s *Store) DeleteOtherSessions(userID, keep string) {
	s.mu.Lock()
	for tok, sess := range s.sessions {
		if sess.UserID == userID && tok != keep {
			delete(s.sessions, tok)
		}
	}
	s.dirtySessions = true
	s.mu.Unlock()
}

func (s *Store) DeleteUserSessions(userID string) {
	s.mu.Lock()
	for tok, sess := range s.sessions {
		if sess.UserID == userID {
			delete(s.sessions, tok)
		}
	}
	s.dirtySessions = true
	s.mu.Unlock()
}

// ---------- Файлы ----------

func (s *Store) AddUpload(m *UploadMeta) {
	s.mu.Lock()
	s.uploads[m.ID] = m
	s.dirtyUploads = true
	s.mu.Unlock()
}

// BindUploads привязывает файлы к чату, в который их отправили.
// До привязки файл доступен только загрузившему.
func (s *Store) BindUploads(ids []string, chatID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, id := range ids {
		if m := s.uploads[id]; m != nil && m.ChatID != chatID {
			m.ChatID = chatID
			s.dirtyUploads = true
		}
	}
}

// migrateUploadChats проставляет chatId старым файлам, которые уже есть
// в сообщениях: раньше привязки не было, и файлы из личных переписок
// открывались по прямой ссылке любому вошедшему.
func (s *Store) migrateUploadChats() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for chatID, chat := range s.chats {
		for _, msg := range chat.Messages {
			for _, f := range msg.Files {
				if m := s.uploads[f.ID]; m != nil && m.ChatID == "" {
					m.ChatID = chatID
					s.dirtyUploads = true
				}
			}
		}
	}
}

// cleanup удаляет просроченные сессии и файлы, загруженные, но так и не
// отправленные (старше суток).
func (s *Store) cleanup() {
	now := time.Now()
	var orphan []string
	s.mu.Lock()
	for token, sess := range s.sessions {
		if now.After(sess.ExpiresAt) {
			delete(s.sessions, token)
			s.dirtySessions = true
		}
	}
	for id, m := range s.uploads {
		if m.ChatID == "" && now.Sub(m.CreatedAt) > 24*time.Hour {
			delete(s.uploads, id)
			s.dirtyUploads = true
			orphan = append(orphan, id)
		}
	}
	s.mu.Unlock()
	for _, id := range orphan {
		if err := os.Remove(filepath.Join(s.upDir, id)); err != nil && !errors.Is(err, os.ErrNotExist) {
			s.log.Printf("уборка файла %s: %v", id, err)
		}
	}
	if len(orphan) > 0 {
		s.log.Printf("уборка: удалено файлов без сообщения: %d", len(orphan))
	}
}

func (s *Store) Upload(id string) *UploadMeta {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.uploads[id]
	if m == nil {
		return nil
	}
	c := *m
	return &c
}

// ---------- Резервные копии ----------

func (s *Store) maybeBackup() {
	s.mu.Lock()
	due := time.Since(s.lastBackup) > 23*time.Hour
	s.mu.Unlock()
	if !due {
		return
	}
	if err := s.Backup(); err != nil {
		s.log.Printf("резервная копия: %v", err)
		return
	}
	s.mu.Lock()
	s.lastBackup = time.Now()
	s.mu.Unlock()
}

// Backup упаковывает метаданные и историю сообщений в zip.
func (s *Store) Backup() error {
	dir := filepath.Join(s.dir, "backups")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	name := filepath.Join(dir, "officechat-"+time.Now().Format("20060102-150405")+".zip")
	tmp := name + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(f)
	add := func(pathInZip string, data []byte) error {
		w, err := zw.Create(pathInZip)
		if err != nil {
			return err
		}
		_, err = w.Write(data)
		return err
	}
	s.mu.Lock()
	var errs []error
	if b, err := json.MarshalIndent(mapUsers(s.users), "", "  "); err == nil {
		errs = append(errs, add("users.json", b))
	}
	if b, err := json.MarshalIndent(mapChannels(s.channels), "", "  "); err == nil {
		errs = append(errs, add("channels.json", b))
	}
	if b, err := json.MarshalIndent(mapUploads(s.uploads), "", "  "); err == nil {
		errs = append(errs, add("uploads.json", b))
	}
	for id, chat := range s.chats {
		if b, err := json.Marshal(chat); err == nil {
			errs = append(errs, add("chats/"+id+".json", b))
		}
	}
	s.mu.Unlock()
	for _, e := range errs {
		if e != nil {
			err = e
			break
		}
	}
	if cerr := zw.Close(); cerr != nil && err == nil {
		err = cerr
	}
	if cerr := f.Close(); cerr != nil && err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, name); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	// Вложения не дублируются в каждой копии: держим одно зеркало папки uploads.
	if err := s.syncUploads(filepath.Join(dir, "uploads")); err != nil {
		s.log.Printf("резервная копия вложений: %v", err)
	}
	// оставляем 10 последних копий
	files, _ := filepath.Glob(filepath.Join(dir, "officechat-*.zip"))
	sort.Strings(files)
	for len(files) > 10 {
		_ = os.Remove(files[0])
		files = files[1:]
	}
	s.log.Printf("резервная копия: %s", filepath.Base(name))
	return nil
}

func uniqueIDs(ids []string) []string {
	seen := make(map[string]bool, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func withoutID(ids []string, id string) []string {
	out := ids[:0:0]
	for _, x := range ids {
		if x != id {
			out = append(out, x)
		}
	}
	return out
}

// UpdateChannelMembers добавляет и удаляет участников закрытого канала.
func (s *Store) UpdateChannelMembers(id string, add, remove []string) (*Channel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.channels[id]
	if c == nil || c.IsDM {
		return nil, errors.New("канал не найден")
	}
	if !c.Private {
		return nil, errors.New("это открытый канал, участники не ограничены")
	}
	for _, uid := range add {
		if s.users[uid] == nil {
			continue
		}
		if !contains(c.Members, uid) {
			c.Members = append(c.Members, uid)
		}
	}
	for _, uid := range remove {
		c.Members = withoutID(c.Members, uid)
	}
	if len(c.Members) == 0 {
		return nil, errors.New("в канале должен остаться хотя бы один участник")
	}
	s.dirtyChannels = true
	cp := *c
	cp.Members = append([]string(nil), c.Members...)
	return &cp, nil
}

// ExportMessages возвращает всю переписку чата в порядке отправки.
func (s *Store) ExportMessages(chatID string) []*Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	chat := s.chats[chatID]
	if chat == nil {
		return []*Message{}
	}
	out := make([]*Message, 0, len(chat.Messages))
	for _, m := range chat.Messages {
		out = append(out, m.Clone())
	}
	return out
}

// syncUploads копирует в резервную папку только новые вложения.
func (s *Store) syncUploads(dst string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(s.upDir)
	if err != nil {
		return err
	}
	copied := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		target := filepath.Join(dst, e.Name())
		if _, err := os.Stat(target); err == nil {
			continue
		}
		src, err := os.Open(filepath.Join(s.upDir, e.Name()))
		if err != nil {
			continue
		}
		tmp := target + ".tmp"
		out, err := os.Create(tmp)
		if err != nil {
			_ = src.Close()
			continue
		}
		_, err = io.Copy(out, src)
		_ = src.Close()
		cerr := out.Close()
		if err != nil || cerr != nil {
			_ = os.Remove(tmp)
			continue
		}
		if err := os.Rename(tmp, target); err == nil {
			copied++
		}
	}
	if copied > 0 {
		s.log.Printf("вложений скопировано в резерв: %d", copied)
	}
	return nil
}
