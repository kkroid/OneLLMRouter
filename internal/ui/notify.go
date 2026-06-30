package ui

import (
	"sync"
	"time"
)

type rateEntry struct {
	until time.Time
}

var (
	rateMu      sync.Mutex
	rateMap     = make(map[string]*rateEntry)
	bellEnabled = true // default on, set via SetBell()
)

// ErrorNotify flashes the tray icon yellow. Same key only once per 5 min.
// Safe to call when no tray exists (no-op).
func ErrorNotify(title, message string) {
	key := title + "\x00" + message
	if !rateOK(key) {
		return
	}
	FlashWarning()
}

// SetBell toggles the system beep on error. Default is true.
func SetBell(on bool) { bellEnabled = on }

func rateOK(key string) bool {
	rateMu.Lock()
	defer rateMu.Unlock()
	now := time.Now()
	if e, ok := rateMap[key]; ok && now.Before(e.until) {
		return false
	}
	rateMap[key] = &rateEntry{until: now.Add(5 * time.Minute)}
	return true
}
