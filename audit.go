package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// AuditEntry — одна запись журнала действий администратора.
type AuditEntry struct {
	At     time.Time `json:"at"`
	Actor  string    `json:"actor"`
	Action string    `json:"action"`
	Target string    `json:"target,omitempty"`
	Detail string    `json:"detail,omitempty"`
}

const (
	auditFile      = "audit.log"
	auditMaxSize   = 1 << 20
	auditReadLimit = 100
)

var auditMu sync.Mutex

// Audit дописывает событие в data/audit.log. Журнал не участвует в
// резервных копиях: это служебная информация, а не данные чата.
func (s *Store) Audit(actor, action, target, detail string) {
	entry := AuditEntry{At: time.Now(), Actor: actor, Action: action, Target: target, Detail: detail}
	line, err := json.Marshal(entry)
	if err != nil {
		return
	}
	line = append(line, '\n')

	auditMu.Lock()
	defer auditMu.Unlock()
	path := filepath.Join(s.dir, auditFile)
	if fi, err := os.Stat(path); err == nil && fi.Size() > auditMaxSize {
		_ = os.Rename(path, path+".1")
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(line)
}

// AuditLog возвращает последние записи журнала, новые сверху.
func (s *Store) AuditLog(limit int) []AuditEntry {
	if limit <= 0 || limit > auditReadLimit {
		limit = auditReadLimit
	}
	auditMu.Lock()
	defer auditMu.Unlock()

	f, err := os.Open(filepath.Join(s.dir, auditFile))
	if err != nil {
		return []AuditEntry{}
	}
	defer f.Close()

	var all []AuditEntry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 256*1024)
	for sc.Scan() {
		var e AuditEntry
		if json.Unmarshal(sc.Bytes(), &e) == nil {
			all = append(all, e)
		}
	}
	if len(all) > limit {
		all = all[len(all)-limit:]
	}
	for i, j := 0, len(all)-1; i < j; i, j = i+1, j-1 {
		all[i], all[j] = all[j], all[i]
	}
	return all
}
