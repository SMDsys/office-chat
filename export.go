package main

import (
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// handleExport отдаёт переписку канала файлом: HTML для чтения или JSON для архива.
func (a *App) handleExport(w http.ResponseWriter, r *http.Request, u *User) {
	id := r.PathValue("id")
	if !a.store.CanAccess(id, u.ID) {
		writeErr(w, 403, "нет доступа к этому чату")
		return
	}
	ch := a.store.Channel(id)
	if ch == nil {
		writeErr(w, 404, "чат не найден")
		return
	}
	msgs := a.store.ExportMessages(id)
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "html"
	}

	base := "officechat-" + slugName(ch.Name) + "-" + time.Now().Format("2006-01-02")
	if format == "json" {
		data, err := json.MarshalIndent(map[string]any{
			"chat":     ch,
			"messages": msgs,
			"exported": time.Now(),
		}, "", "  ")
		if err != nil {
			writeErr(w, 500, "не удалось собрать файл")
			return
		}
		writeDownload(w, base+".json", "application/json; charset=utf-8", data)
		return
	}
	if format != "html" {
		writeErr(w, 400, "формат: html или json")
		return
	}

	names := map[string]string{}
	for _, x := range a.store.Users() {
		names[x.ID] = x.Name
	}
	var b strings.Builder
	b.WriteString("<!doctype html><html lang=\"ru\"><head><meta charset=\"utf-8\">")
	b.WriteString("<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">")
	fmt.Fprintf(&b, "<title>%s — Офис-чат</title>", html.EscapeString(ch.Name))
	b.WriteString(`<style>
body{font:15px/1.5 -apple-system,Segoe UI,Roboto,sans-serif;max-width:820px;margin:32px auto;padding:0 16px;color:#1f2328}
h1{font-size:22px;margin:0 0 4px}
.meta{color:#6b7280;font-size:13px;margin-bottom:24px}
.msg{border-top:1px solid #e5e7eb;padding:12px 0}
.author{font-weight:600}
.time{color:#6b7280;font-size:12px;margin-left:8px}
.text{white-space:pre-wrap;margin-top:4px}
.files{margin-top:6px;font-size:13px;color:#374151}
</style></head><body>`)
	fmt.Fprintf(&b, "<h1>%s</h1>", html.EscapeString(ch.Name))
	if ch.Topic != "" {
		fmt.Fprintf(&b, "<div class=\"meta\">%s</div>", html.EscapeString(ch.Topic))
	}
	fmt.Fprintf(&b, "<div class=\"meta\">Экспорт от %s · сообщений: %d</div>", time.Now().Format("02.01.2006 15:04"), len(msgs))
	for _, m := range msgs {
		author := names[m.UserID]
		if author == "" {
			author = "Сотрудник"
		}
		b.WriteString("<div class=\"msg\">")
		fmt.Fprintf(&b, "<span class=\"author\">%s</span><span class=\"time\">%s</span>",
			html.EscapeString(author), m.CreatedAt.Local().Format("02.01.2006 15:04"))
		if m.Deleted {
			b.WriteString("<div class=\"text\"><i>сообщение удалено</i></div>")
		} else {
			fmt.Fprintf(&b, "<div class=\"text\">%s</div>", html.EscapeString(m.Text))
			if len(m.Files) > 0 {
				var fs []string
				for _, f := range m.Files {
					fs = append(fs, html.EscapeString(f.Name))
				}
				fmt.Fprintf(&b, "<div class=\"files\">Файлы: %s</div>", strings.Join(fs, ", "))
			}
		}
		b.WriteString("</div>")
	}
	b.WriteString("</body></html>")
	writeDownload(w, base+".html", "text/html; charset=utf-8", []byte(b.String()))
}

func writeDownload(w http.ResponseWriter, name, contentType string, data []byte) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", `attachment; filename="`+asciiFallback(name)+`"; filename*=UTF-8''`+url.PathEscape(name))
	w.WriteHeader(200)
	_, _ = w.Write(data)
}

func slugName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_':
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		out = "chat"
	}
	if len([]rune(out)) > 40 {
		out = string([]rune(out)[:40])
	}
	return out
}
