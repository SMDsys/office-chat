package main

import "net/http"

// handleChannelReadOnly включает режим объявлений: пишут создатель и админы.
func (a *App) handleChannelReadOnly(w http.ResponseWriter, r *http.Request, u *User) {
	id := r.PathValue("id")
	ch := a.store.Channel(id)
	if ch == nil || ch.IsDM {
		writeErr(w, 404, "канал не найден")
		return
	}
	if ch.CreatedBy != u.ID && !u.IsAdmin {
		writeErr(w, 403, "менять режим канала может только создатель или администратор")
		return
	}
	var req struct {
		ReadOnly bool `json:"readOnly"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	updated, err := a.store.SetChannelReadOnly(id, req.ReadOnly)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	ev := Event{Type: "chat.update", Data: chatView{Channel: *updated}}
	if updated.Private {
		a.hub.Publish(ev, func(uid string) bool { return contains(updated.Members, uid) })
	} else {
		a.hub.Broadcast(ev)
	}
	detail := "объявления"
	if !req.ReadOnly {
		detail = "обычный"
	}
	a.store.Audit(u.Login, "channel_readonly", updated.Name, detail)
	writeJSON(w, 200, map[string]any{"chat": updated})
}
