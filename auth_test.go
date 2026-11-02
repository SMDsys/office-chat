package main

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPasswordHash(t *testing.T) {
	salt := newSalt()
	hash, err := hashPassword("секрет123", salt)
	if err != nil {
		t.Fatal(err)
	}
	if !checkPassword("секрет123", salt, hash) {
		t.Fatal("верный пароль отклонён")
	}
	if checkPassword("секрет124", salt, hash) {
		t.Fatal("неверный пароль принят")
	}
	if checkPassword("секрет123", newSalt(), hash) {
		t.Fatal("пароль принят с чужой солью")
	}
}

func TestLimiter(t *testing.T) {
	l := newLimiter()
	for i := 0; i < 3; i++ {
		if !l.Allow("k", 3, time.Minute) {
			t.Fatalf("попытка %d отклонена раньше времени", i+1)
		}
	}
	if l.Allow("k", 3, time.Minute) {
		t.Fatal("лимит не сработал")
	}
	if !l.Allow("другой", 3, time.Minute) {
		t.Fatal("лимит одного ключа повлиял на другой")
	}
	l.Reset("k")
	if !l.Allow("k", 3, time.Minute) {
		t.Fatal("после сброса лимит не снялся")
	}
}

func TestSameOrigin(t *testing.T) {
	r := httptest.NewRequest("POST", "http://192.168.1.10:8080/api/login", nil)
	r.Header.Set("Origin", "http://192.168.1.10:8080")
	if !sameOrigin(r) {
		t.Fatal("свой Origin отклонён")
	}
	r.Header.Set("Origin", "http://evil.example")
	if sameOrigin(r) {
		t.Fatal("чужой Origin принят")
	}
}

func TestSafeFileName(t *testing.T) {
	cases := map[string]string{
		"../../etc/passwd":   "passwd",
		`C:\Windows\win.ini`: "win.ini",
		"..":                 "файл",
		"отчёт 2026.pdf":     "отчёт 2026.pdf",
	}
	for in, want := range cases {
		if got := safeFileName(in); got != want {
			t.Errorf("safeFileName(%q) = %q, ожидалось %q", in, got, want)
		}
	}
	if strings.ContainsRune(safeFileName("a\x00b"), 0) {
		t.Error("управляющий символ остался в имени")
	}
}
