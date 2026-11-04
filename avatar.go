package main

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const maxAvatar = 2 << 20 // 2 МБ

var avatarExt = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/gif":  ".gif",
	"image/webp": ".webp",
}

var avatarMime = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
}

func (a *App) avatarDir() string { return filepath.Join(a.store.dir, "avatars") }

// handleAvatarUpload принимает фото профиля: PNG, JPEG, GIF или WebP до 2 МБ.
func (a *App) handleAvatarUpload(w http.ResponseWriter, r *http.Request, u *User) {
	if !a.limiter.Allow("avatar:"+u.ID, 10, 10*time.Minute) {
		writeErr(w, http.StatusTooManyRequests, "слишком часто, попробуйте позже")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxAvatar+(1<<20))
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		writeErr(w, 400, "не удалось прочитать файл (до 2 МБ)")
		return
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()
	src, _, err := r.FormFile("file")
	if err != nil {
		writeErr(w, 400, "файл не передан")
		return
	}
	defer src.Close()
	data, err := io.ReadAll(io.LimitReader(src, maxAvatar+1))
	if err != nil {
		writeErr(w, 400, "не удалось прочитать файл")
		return
	}
	if len(data) == 0 {
		writeErr(w, 400, "файл пустой")
		return
	}
	if len(data) > maxAvatar {
		writeErr(w, 413, "фото больше 2 МБ")
		return
	}
	mimeType := http.DetectContentType(data)
	ext, ok := avatarExt[mimeType]
	if !ok {
		writeErr(w, 400, "нужен PNG, JPEG, GIF или WebP")
		return
	}
	dir := a.avatarDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		writeErr(w, 500, "не удалось сохранить фото")
		return
	}
	name := u.ID + ext
	tmp := filepath.Join(dir, name+".tmp")
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		writeErr(w, 500, "не удалось сохранить фото")
		return
	}
	if err := os.Rename(tmp, filepath.Join(dir, name)); err != nil {
		_ = os.Remove(tmp)
		writeErr(w, 500, "не удалось сохранить фото")
		return
	}
	if u.Avatar != "" && u.Avatar != name {
		_ = os.Remove(filepath.Join(dir, filepath.Base(u.Avatar)))
	}
	updated, ok := a.store.UpdateUser(u.ID, func(x *User) { x.Avatar = name })
	if !ok {
		writeErr(w, 404, "пользователь не найден")
		return
	}
	a.hub.Broadcast(Event{Type: "user.update", Data: updated.Public(a.hub.IsOnline(u.ID))})
	writeJSON(w, 200, map[string]any{"user": updated.Public(a.hub.IsOnline(u.ID))})
}

func (a *App) handleAvatarDelete(w http.ResponseWriter, r *http.Request, u *User) {
	if u.Avatar != "" {
		_ = os.Remove(filepath.Join(a.avatarDir(), filepath.Base(u.Avatar)))
	}
	updated, ok := a.store.UpdateUser(u.ID, func(x *User) { x.Avatar = "" })
	if !ok {
		writeErr(w, 404, "пользователь не найден")
		return
	}
	a.hub.Broadcast(Event{Type: "user.update", Data: updated.Public(a.hub.IsOnline(u.ID))})
	writeJSON(w, 200, map[string]any{"user": updated.Public(a.hub.IsOnline(u.ID))})
}

// handleAvatarFile отдаёт фото профиля вошедшим пользователям.
func (a *App) handleAvatarFile(w http.ResponseWriter, r *http.Request) {
	if a.store.SessionUser(sessionToken(r)) == nil {
		writeErr(w, http.StatusUnauthorized, "нужно войти в чат")
		return
	}
	raw := r.PathValue("file")
	name := filepath.Base(raw)
	if name != raw || strings.Contains(name, "..") || name == "." {
		writeErr(w, 404, "фото не найдено")
		return
	}
	f, err := os.Open(filepath.Join(a.avatarDir(), name))
	if err != nil {
		writeErr(w, 404, "фото не найдено")
		return
	}
	defer f.Close()
	if ct, ok := avatarMime[strings.ToLower(filepath.Ext(name))]; ok {
		w.Header().Set("Content-Type", ct)
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	http.ServeContent(w, r, name, time.Time{}, f)
}
