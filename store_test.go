package main

import (
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
