package main

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	passIterations = 120_000
	passKeyLen     = 32
	cookieName     = "oc_session"
)

func randomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func hashPassword(password, saltB64 string) (string, error) {
	salt, err := base64.RawStdEncoding.DecodeString(saltB64)
	if err != nil {
		return "", err
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, passIterations, passKeyLen)
	if err != nil {
		return "", err
	}
	return base64.RawStdEncoding.EncodeToString(key), nil
}

func newSalt() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawStdEncoding.EncodeToString(b)
}

func checkPassword(password, saltB64, hashB64 string) bool {
	got, err := hashPassword(password, saltB64)
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(hashB64)) == 1
}

// ---------- Ограничение частоты попыток входа ----------

type attempt struct {
	count int
	until time.Time
}

type limiter struct {
	mu sync.Mutex
	m  map[string]*attempt
}

func newLimiter() *limiter { return &limiter{m: map[string]*attempt{}} }

// Allow разрешает не более n попыток за window.
func (l *limiter) Allow(key string, n int, window time.Duration) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	a := l.m[key]
	if a == nil || now.After(a.until) {
		l.m[key] = &attempt{count: 1, until: now.Add(window)}
		return true
	}
	if a.count >= n {
		return false
	}
	a.count++
	return true
}

func (l *limiter) Reset(key string) {
	l.mu.Lock()
	delete(l.m, key)
	l.mu.Unlock()
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// ---------- Сессия в cookie ----------

func setSessionCookie(w http.ResponseWriter, token string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionTTL / time.Second),
	})
}

func (a *App) setSessionCookie(w http.ResponseWriter, token string) {
	setSessionCookie(w, token, a.secure)
}

func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

func sessionToken(r *http.Request) string {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return ""
	}
	return c.Value
}

// sameOrigin защищает от подделки запросов с чужих сайтов.
func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Host, r.Host)
}

// validName проверяет отображаемое имя.
func validName(name string) bool {
	name = strings.TrimSpace(name)
	return runeLen(name) >= 1 && runeLen(name) <= 60
}
