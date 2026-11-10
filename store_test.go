package main

import (
	"crypto/tls"
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	s, err := NewStore(t.TempDir(), log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(s.Close)
	return s
}

func TestMigrateUploadChats(t *testing.T) {
	s := testStore(t)
	u, err := s.CreateUser("ivan", "Иван", "hash", "salt")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	dm, err := s.CreateChannel("Личные", "", u.ID)
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}
	up := &UploadMeta{Attachment: Attachment{ID: "f1", Name: "a.txt"}, UserID: u.ID, CreatedAt: time.Now()}
	s.AddUpload(up)
	s.AddMessage(&Message{ID: "m1", ChatID: dm.ID, UserID: u.ID, Text: "файл", CreatedAt: time.Now(),
		Files: []Attachment{up.Attachment}})

	s.migrateUploadChats()

	if got := s.Upload("f1"); got == nil || got.ChatID != dm.ID {
		t.Fatalf("файл не привязан к чату: %+v", got)
	}
}

func TestCleanupRemovesExpiredSessions(t *testing.T) {
	s := testStore(t)
	u, err := s.CreateUser("ivan", "Иван", "hash", "salt")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	sess, err := s.CreateSession(u.ID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	s.mu.Lock()
	s.sessions[sess.Token].ExpiresAt = time.Now().Add(-time.Hour)
	s.mu.Unlock()

	s.cleanup()

	if s.SessionUser(sess.Token) != nil {
		t.Fatal("просроченная сессия осталась")
	}
}

func TestCleanupRemovesOrphanUploads(t *testing.T) {
	s := testStore(t)
	u, err := s.CreateUser("ivan", "Иван", "hash", "salt")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	up := &UploadMeta{Attachment: Attachment{ID: "old", Name: "a.txt"}, UserID: u.ID,
		CreatedAt: time.Now().Add(-48 * time.Hour)}
	s.AddUpload(up)
	path := filepath.Join(s.upDir, up.ID)
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	s.cleanup()

	if s.Upload("old") != nil {
		t.Fatal("файл без сообщения остался в метаданных")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("файл без сообщения остался на диске")
	}
}

func TestCleanupKeepsAttachedUploads(t *testing.T) {
	s := testStore(t)
	u, err := s.CreateUser("ivan", "Иван", "hash", "salt")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	up := &UploadMeta{Attachment: Attachment{ID: "sent", Name: "a.txt"}, UserID: u.ID,
		ChatID: generalChannelID, CreatedAt: time.Now().Add(-48 * time.Hour)}
	s.AddUpload(up)

	s.cleanup()

	if s.Upload("sent") == nil {
		t.Fatal("файл из сообщения удалён")
	}
}

func TestMarkAllRead(t *testing.T) {
	s := testStore(t)
	u1, err := s.CreateUser("user1", "Первый", "hash", "salt")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	u3, err := s.CreateUser("user3", "Третий", "hash", "salt")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	dm, err := s.CreateChannel("Личные", "", u1.ID)
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}
	s.mu.Lock()
	s.channels[dm.ID].IsDM = true
	s.channels[dm.ID].Members = []string{u1.ID, "u2"}
	s.mu.Unlock()

	at := time.Now()
	s.MarkAllRead(u1.ID, at)

	s.mu.Lock()
	gotGeneral, gotDM := s.users[u1.ID].Reads[generalChannelID], s.users[u1.ID].Reads[dm.ID]
	s.mu.Unlock()
	if !gotGeneral.Equal(at) || !gotDM.Equal(at) {
		t.Fatalf("не все доступные чаты отмечены: general=%v dm=%v", gotGeneral, gotDM)
	}

	s.MarkAllRead(u3.ID, at)
	s.mu.Lock()
	_, hasDM := s.users[u3.ID].Reads[dm.ID]
	_, hasGeneral := s.users[u3.ID].Reads[generalChannelID]
	s.mu.Unlock()
	if hasDM {
		t.Fatal("чужой личный чат отмечен прочитанным")
	}
	if !hasGeneral {
		t.Fatal("общий канал не отмечен")
	}
}

func TestCanAccessDM(t *testing.T) {
	s := testStore(t)
	dm, err := s.CreateChannel("Личные", "", "u1")
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}
	s.mu.Lock()
	s.channels[dm.ID].IsDM = true
	s.channels[dm.ID].Members = []string{"u1", "u2"}
	s.mu.Unlock()

	if !s.CanAccess(dm.ID, "u1") || !s.CanAccess(dm.ID, "u2") {
		t.Fatal("участник не получил доступ к личному чату")
	}
	if s.CanAccess(dm.ID, "u3") {
		t.Fatal("посторонний получил доступ к личному чату")
	}
	if !s.CanAccess(generalChannelID, "u3") {
		t.Fatal("общий канал должен быть доступен всем")
	}
}

func TestAuditLog(t *testing.T) {
	s := testStore(t)
	s.Audit("ivan", "reset", "petr", "")
	s.Audit("ivan", "block", "petr", "")
	s.Audit("petr", "register", "", "")

	entries := s.AuditLog(2)
	if len(entries) != 2 {
		t.Fatalf("ожидали 2 записи, получили %d", len(entries))
	}
	if entries[0].Action != "register" || entries[0].Actor != "petr" {
		t.Fatalf("новая запись должна быть первой: %+v", entries[0])
	}
	if entries[1].Action != "block" || entries[1].Target != "petr" {
		t.Fatalf("вторая запись не совпала: %+v", entries[1])
	}
	if all := s.AuditLog(100); len(all) != 3 {
		t.Fatalf("ожидали 3 записи, получили %d", len(all))
	}
}

func TestPrivateChannelAccess(t *testing.T) {
	s := testStore(t)
	u1, _ := s.CreateUser("user1", "Первый", "hash", "salt")
	u2, _ := s.CreateUser("user2", "Второй", "hash", "salt")
	u3, _ := s.CreateUser("user3", "Третий", "hash", "salt")
	ch, err := s.CreateChannelWith("Бухгалтерия", "", u1.ID, true, []string{u2.ID})
	if err != nil {
		t.Fatalf("CreateChannelWith: %v", err)
	}
	if !s.CanAccess(ch.ID, u1.ID) || !s.CanAccess(ch.ID, u2.ID) {
		t.Fatal("участники закрытого канала должны иметь доступ")
	}
	if s.CanAccess(ch.ID, u3.ID) {
		t.Fatal("посторонний получил доступ к закрытому каналу")
	}
	for _, c := range s.ChannelsFor(u3.ID) {
		if c.ID == ch.ID {
			t.Fatal("закрытый канал попал в список постороннего")
		}
	}
	if _, err := s.UpdateChannelMembers(ch.ID, []string{u3.ID}, nil); err != nil {
		t.Fatalf("UpdateChannelMembers add: %v", err)
	}
	if !s.CanAccess(ch.ID, u3.ID) {
		t.Fatal("после добавления участник не получил доступ")
	}
	if _, err := s.UpdateChannelMembers(ch.ID, nil, []string{u1.ID, u2.ID, u3.ID}); err == nil {
		t.Fatal("нельзя оставить канал без участников")
	}
}

func TestDeleteOtherSessions(t *testing.T) {
	s := testStore(t)
	u, _ := s.CreateUser("user1", "Первый", "hash", "salt")
	a, _ := s.CreateSession(u.ID)
	b, _ := s.CreateSession(u.ID)
	s.DeleteOtherSessions(u.ID, a.Token)
	if s.SessionUser(a.Token) == nil {
		t.Fatal("текущая сессия должна сохраниться")
	}
	if s.SessionUser(b.Token) != nil {
		t.Fatal("другая сессия должна быть удалена")
	}
}

func TestPrivateChannelSearchHidden(t *testing.T) {
	s := testStore(t)
	u1, _ := s.CreateUser("user1", "Первый", "hash", "salt")
	u2, _ := s.CreateUser("user2", "Второй", "hash", "salt")
	u3, _ := s.CreateUser("user3", "Третий", "hash", "salt")
	ch, err := s.CreateChannelWith("Секреты", "", u1.ID, true, []string{u2.ID})
	if err != nil {
		t.Fatalf("CreateChannelWith: %v", err)
	}
	s.AddMessage(&Message{ID: "m1", ChatID: ch.ID, UserID: u1.ID, Text: "секретный текст", CreatedAt: time.Now()})

	for _, m := range s.Search(SearchOptions{UserID: u3.ID, Query: "секретный", Limit: 100}) {
		t.Fatalf("посторонний нашёл сообщение закрытого канала: %q", m.Text)
	}
	if got := s.Search(SearchOptions{UserID: u3.ID, Query: "секретный", ChatID: ch.ID, Limit: 100}); len(got) != 0 {
		t.Fatalf("поиск по закрытому каналу вернул %d сообщений постороннему", len(got))
	}
	if got := s.Search(SearchOptions{UserID: u2.ID, Query: "секретный", Limit: 100}); len(got) != 1 {
		t.Fatalf("участник должен найти сообщение, найдено %d", len(got))
	}
}

func TestSelfSignedCoversLocalAddresses(t *testing.T) {
	certPEM, keyPEM, err := selfSignedPEM()
	if err != nil {
		t.Fatalf("selfSignedPEM: %v", err)
	}
	c, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("X509KeyPair: %v", err)
	}
	if !certUsable(c) {
		t.Fatal("свежий сертификат должен покрывать localhost и текущие IP")
	}
}

func TestChatMute(t *testing.T) {
	s := testStore(t)
	u, _ := s.CreateUser("user1", "Первый", "hash", "salt")
	if _, ok := s.SetChatMute(u.ID, "general", "mentions"); !ok {
		t.Fatal("SetChatMute: пользователь не найден")
	}
	got := s.User(u.ID)
	if got.Mutes["general"] != "mentions" {
		t.Fatalf("ожидался режим mentions, получено %q", got.Mutes["general"])
	}
	if _, ok := s.SetChatMute(u.ID, "general", ""); !ok {
		t.Fatal("SetChatMute: сброс не удался")
	}
	if _, exists := s.User(u.ID).Mutes["general"]; exists {
		t.Fatal("пустой режим должен удалять настройку")
	}
}

func TestSearchByAttachmentAndFilters(t *testing.T) {
	s := testStore(t)
	u1, err := s.CreateUser("user1", "Один", "hash", "salt")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	u2, err := s.CreateUser("user2", "Два", "hash", "salt")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	ch, err := s.CreateChannelWith("Тестовый", "", u1.ID, false, nil)
	if err != nil {
		t.Fatalf("CreateChannelWith: %v", err)
	}
	now := time.Now()
	s.AddMessage(&Message{
		ID: "m1", ChatID: ch.ID, UserID: u1.ID, Text: "посмотри файл",
		Files:     []Attachment{{ID: "f1", Name: "Договор поставки.pdf"}},
		CreatedAt: now,
	})
	s.AddMessage(&Message{ID: "m2", ChatID: ch.ID, UserID: u2.ID, Text: "договор обсудили", CreatedAt: now.Add(-time.Hour)})

	if got := s.Search(SearchOptions{UserID: u1.ID, Query: "договор", Limit: 10}); len(got) != 2 {
		t.Fatalf("поиск по тексту и имени файла: получено %d", len(got))
	}
	if got := s.Search(SearchOptions{UserID: u1.ID, Query: "договор", AuthorID: u2.ID, Limit: 10}); len(got) != 1 || got[0].ID != "m2" {
		t.Fatalf("фильтр по автору: %v", got)
	}
	if got := s.Search(SearchOptions{UserID: u1.ID, Query: "договор", From: now.Add(-30 * time.Minute), Limit: 10}); len(got) != 1 || got[0].ID != "m1" {
		t.Fatalf("фильтр по дате: %v", got)
	}
}

func TestSetChannelReadOnly(t *testing.T) {
	s := testStore(t)
	u, err := s.CreateUser("user1", "Один", "hash", "salt")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	ch, err := s.CreateChannelWith("Новости", "", u.ID, false, nil)
	if err != nil {
		t.Fatalf("CreateChannelWith: %v", err)
	}
	updated, err := s.SetChannelReadOnly(ch.ID, true)
	if err != nil {
		t.Fatalf("SetChannelReadOnly: %v", err)
	}
	if !updated.ReadOnly || !s.Channel(ch.ID).ReadOnly {
		t.Fatal("режим объявлений не сохранился")
	}
}
