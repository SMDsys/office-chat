package main

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
	"time"
)

// ---------- Пользователь ----------

type User struct {
	ID        string               `json:"id"`
	Login     string               `json:"login"`
	Name      string               `json:"name"`
	PassHash  string               `json:"passHash"`
	Salt      string               `json:"salt"`
	Color     string               `json:"color"`
	IsAdmin   bool                 `json:"isAdmin"`
	Blocked   bool                 `json:"blocked"`
	CreatedAt time.Time            `json:"createdAt"`
	LastSeen  time.Time            `json:"lastSeen"`
	Reads     map[string]time.Time `json:"reads,omitempty"`
	Avatar    string               `json:"avatar,omitempty"`
	Status    string               `json:"status,omitempty"` // "" — активен, "away", "dnd"
}

// PublicUser — то, что можно отдавать клиенту (без хэша пароля).
type PublicUser struct {
	ID       string    `json:"id"`
	Login    string    `json:"login"`
	Name     string    `json:"name"`
	Color    string    `json:"color"`
	IsAdmin  bool      `json:"isAdmin"`
	Blocked  bool      `json:"blocked"`
	Online   bool      `json:"online"`
	LastSeen time.Time `json:"lastSeen"`
	Avatar   string    `json:"avatar,omitempty"`
	Status   string    `json:"status,omitempty"`
}

func (u *User) Public(online bool) PublicUser {
	return PublicUser{
		ID:       u.ID,
		Login:    u.Login,
		Name:     u.Name,
		Color:    u.Color,
		IsAdmin:  u.IsAdmin,
		Blocked:  u.Blocked,
		Online:   online,
		LastSeen: u.LastSeen,
		Avatar:   u.Avatar,
		Status:   u.Status,
	}
}

// ---------- Канал / личный чат ----------

type Channel struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Topic     string    `json:"topic"`
	IsDM      bool      `json:"isDm"`
	Private   bool      `json:"private,omitempty"` // закрытый канал: доступ только участникам
	Members   []string  `json:"members,omitempty"` // DM и закрытые каналы
	CreatedBy string    `json:"createdBy"`
	CreatedAt time.Time `json:"createdAt"`
}

// ---------- Сообщение ----------

type Attachment struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Size int64  `json:"size"`
	Mime string `json:"mime"`
}

type Message struct {
	ID        string              `json:"id"`
	ChatID    string              `json:"chatId"`
	UserID    string              `json:"userId"`
	Text      string              `json:"text"`
	CreatedAt time.Time           `json:"createdAt"`
	EditedAt  *time.Time          `json:"editedAt,omitempty"`
	Deleted   bool                `json:"deleted,omitempty"`
	ReplyTo   string              `json:"replyTo,omitempty"`
	Files     []Attachment        `json:"files,omitempty"`
	Reactions map[string][]string `json:"reactions,omitempty"`
	Pinned    bool                `json:"pinned,omitempty"`
}

func (m *Message) Clone() *Message {
	if m == nil {
		return nil
	}
	c := *m
	if m.EditedAt != nil {
		t := *m.EditedAt
		c.EditedAt = &t
	}
	if m.Files != nil {
		c.Files = append([]Attachment(nil), m.Files...)
	}
	if m.Reactions != nil {
		c.Reactions = make(map[string][]string, len(m.Reactions))
		for k, v := range m.Reactions {
			c.Reactions[k] = append([]string(nil), v...)
		}
	}
	return &c
}

// ---------- Сессия ----------

type Session struct {
	Token     string    `json:"token"`
	UserID    string    `json:"userId"`
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// ---------- Метаданные загруженных файлов ----------

type UploadMeta struct {
	Attachment
	UserID    string    `json:"userId"`
	ChatID    string    `json:"chatId,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// ---------- Служебное ----------

func randomID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

var userColors = []string{
	"#E8A33D", "#7FB08A", "#7FA7C9", "#C98FA8", "#B9A26B",
	"#8E9BD6", "#D08A6A", "#6FB3AE", "#A98BC4", "#C4A35A",
}

func colorFor(id string) string {
	var sum int
	for _, r := range id {
		sum = sum*31 + int(r)
	}
	if sum < 0 {
		sum = -sum
	}
	return userColors[sum%len(userColors)]
}

func cleanText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.TrimSpace(s)
}

func runeLen(s string) int { return len([]rune(s)) }

func cutRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
