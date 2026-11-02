package main

import (
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

const maxUpload = 50 << 20 // 50 МБ

func safeFileName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	var b strings.Builder
	for _, r := range name {
		if unicode.IsControl(r) || r == '/' || r == '\\' {
			continue
		}
		b.WriteRune(r)
	}
	name = strings.TrimSpace(b.String())
	name = strings.Trim(name, ".")
	if name == "" {
		name = "файл"
	}
	r := []rune(name)
	if len(r) > 120 {
		ext := filepath.Ext(name)
		base := []rune(strings.TrimSuffix(name, ext))
		if len(base) > 100 {
			base = base[:100]
		}
		name = string(base) + ext
	}
	return name
}

func (a *App) handleUpload(w http.ResponseWriter, r *http.Request, u *User) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload+(2<<20))
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		writeErr(w, 400, "не удалось прочитать файл (возможно, он больше 50 МБ)")
		return
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()
	src, hdr, err := r.FormFile("file")
	if err != nil {
		writeErr(w, 400, "файл не передан")
		return
	}
	defer src.Close()

	name := safeFileName(hdr.Filename)
	id := randomID()
	dstPath := filepath.Join(a.store.upDir, id)
	dst, err := os.Create(dstPath)
	if err != nil {
		a.log.Printf("загрузка: %v", err)
		writeErr(w, 500, "не удалось сохранить файл")
		return
	}
	n, copyErr := io.Copy(dst, io.LimitReader(src, maxUpload+1))
	closeErr := dst.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(dstPath)
		writeErr(w, 500, "не удалось сохранить файл")
		return
	}
	if n > maxUpload {
		_ = os.Remove(dstPath)
		writeErr(w, 413, "файл больше 50 МБ")
		return
	}
	if n == 0 {
		_ = os.Remove(dstPath)
		writeErr(w, 400, "файл пустой")
		return
	}
	mimeType := hdr.Header.Get("Content-Type")
	if mimeType == "" || mimeType == "application/octet-stream" {
		if t := mime.TypeByExtension(strings.ToLower(filepath.Ext(name))); t != "" {
			mimeType = t
		} else {
			mimeType = "application/octet-stream"
		}
	}
	meta := &UploadMeta{
		Attachment: Attachment{ID: id, Name: name, Size: n, Mime: mimeType},
		UserID:     u.ID,
		CreatedAt:  time.Now(),
	}
	a.store.AddUpload(meta)
	writeJSON(w, 200, meta.Attachment)
}

func (a *App) handleFile(w http.ResponseWriter, r *http.Request) {
	u := a.store.SessionUser(sessionToken(r))
	if u == nil {
		writeErr(w, http.StatusUnauthorized, "нужно войти в чат")
		return
	}
	id := r.PathValue("id")
	meta := a.store.Upload(id)
	if meta == nil {
		writeErr(w, 404, "файл не найден")
		return
	}
	// Файл, отправленный в чат, видят только участники этого чата.
	// Ещё не отправленный файл доступен только тому, кто его загрузил.
	if meta.ChatID != "" {
		if !a.store.CanAccess(meta.ChatID, u.ID) {
			writeErr(w, http.StatusForbidden, "нет доступа к файлу")
			return
		}
	} else if meta.UserID != u.ID {
		writeErr(w, http.StatusForbidden, "нет доступа к файлу")
		return
	}
	f, err := os.Open(filepath.Join(a.store.upDir, meta.ID))
	if err != nil {
		writeErr(w, 404, "файл не найден на диске")
		return
	}
	defer f.Close()

	w.Header().Set("Content-Type", meta.Mime)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	disposition := "attachment"
	if strings.HasPrefix(meta.Mime, "image/") || meta.Mime == "application/pdf" || strings.HasPrefix(meta.Mime, "text/") {
		disposition = "inline"
	}
	w.Header().Set("Content-Disposition", disposition+`; filename="`+asciiFallback(meta.Name)+`"; filename*=UTF-8''`+url.PathEscape(meta.Name))
	http.ServeContent(w, r, meta.Name, meta.CreatedAt, f)
}

func asciiFallback(name string) string {
	var b strings.Builder
	for _, r := range name {
		if r < 32 || r > 126 || r == '"' || r == '\\' {
			b.WriteByte('_')
			continue
		}
		b.WriteRune(r)
	}
	out := b.String()
	if out == "" || strings.Trim(out, "_") == "" {
		out = "file"
	}
	return out
}
