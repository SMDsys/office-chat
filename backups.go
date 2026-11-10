package main

import (
	"fmt"
	"net/http"
	"os"
)

// handleAdminBackups отдаёт список копий и занятое место.
func (a *App) handleAdminBackups(w http.ResponseWriter, r *http.Request, u *User) {
	if !a.requireAdmin(w, u) {
		return
	}
	list, total := a.store.Backups()
	free, diskTotal := diskStats(a.store.dir)
	writeJSON(w, 200, map[string]any{
		"backups":     list,
		"backupBytes": total,
		"uploadBytes": dirSize(a.store.upDir),
		"freeBytes":   free,
		"diskBytes":   diskTotal,
	})
}

// handleAdminBackupCreate делает копию по требованию администратора.
func (a *App) handleAdminBackupCreate(w http.ResponseWriter, r *http.Request, u *User) {
	if !a.requireAdmin(w, u) {
		return
	}
	if err := a.store.Backup(); err != nil {
		writeErr(w, 500, "не удалось создать копию: "+err.Error())
		return
	}
	a.store.Audit(u.Login, "backup", "", "")
	list, total := a.store.Backups()
	writeJSON(w, 200, map[string]any{"backups": list, "backupBytes": total})
}

// handleAdminBackupDownload отдаёт архив копии только администратору.
func (a *App) handleAdminBackupDownload(w http.ResponseWriter, r *http.Request, u *User) {
	if !a.requireAdmin(w, u) {
		return
	}
	name := r.PathValue("name")
	full, ok := a.store.BackupPath(name)
	if !ok {
		writeErr(w, 404, "копия не найдена")
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeFile(w, r, full)
}

// dirSize считает размер файлов в папке, не заходя в подпапки.
func dirSize(dir string) int64 {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	var total int64
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if info, err := e.Info(); err == nil {
			total += info.Size()
		}
	}
	return total
}

// humanBytes печатает размер по-русски: 512 КБ, 1,4 ГБ.
func humanBytes(v int64) string {
	if v < 1024 {
		return fmt.Sprintf("%d Б", v)
	}
	units := []string{"КБ", "МБ", "ГБ", "ТБ"}
	f := float64(v)
	i := -1
	for f >= 1024 && i < len(units)-1 {
		f /= 1024
		i++
	}
	if f >= 10 {
		return fmt.Sprintf("%.0f %s", f, units[i])
	}
	return fmt.Sprintf("%.1f %s", f, units[i])
}
