package log

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
)

type contextKey string

const (
	requestIDKey  contextKey = "request_id"
	requestMetaKey contextKey = "request_meta"
)

// RequestMeta holds per-request metadata for logging.
type RequestMeta struct {
	Model        string
	Stream       bool
	TTFBMs       int64  // time to first byte in ms (relative)
	Provider     string // "cp" or "ds"
	Error        string // non-empty = request failed
	start        time.Time
}

// MarkStart records the request start time for TTFB calculation.
func (m *RequestMeta) MarkStart() {
	m.start = time.Now()
}

// MarkFirstByte sets TTFB if not already set.
func (m *RequestMeta) MarkFirstByte() {
	if m.TTFBMs == 0 {
		m.TTFBMs = time.Since(m.start).Milliseconds()
	}
}

// WithRequestMeta stores metadata on context. Thread-safe via value semantics.
func WithRequestMeta(ctx context.Context, m *RequestMeta) context.Context {
	return context.WithValue(ctx, requestMetaKey, m)
}

// RequestMetaFromContext retrieves the request metadata.
func RequestMetaFromContext(ctx context.Context) *RequestMeta {
	if m, ok := ctx.Value(requestMetaKey).(*RequestMeta); ok {
		return m
	}
	return &RequestMeta{}
}

// dailyWriter rotates log files by date (midnight local time).
type dailyWriter struct {
	mu       sync.Mutex
	dir      string
	today    string
	file     *os.File
}

func newDailyWriter(dir string) (*dailyWriter, error) {
	dw := &dailyWriter{dir: dir}
	if err := dw.rotate(); err != nil {
		return nil, err
	}
	return dw, nil
}

func (dw *dailyWriter) Write(p []byte) (n int, err error) {
	dw.mu.Lock()
	defer dw.mu.Unlock()

	today := time.Now().Format("2006-01-02")
	if today != dw.today {
		dw.rotate()
	}
	return dw.file.Write(p)
}

func (dw *dailyWriter) rotate() error {
	if dw.file != nil {
		dw.file.Close()
	}
	dw.today = time.Now().Format("2006-01-02")
	name := filepath.Join(dw.dir, "onellm-router-"+dw.today+".log")
	f, err := os.OpenFile(name, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open log %s: %w", name, err)
	}
	dw.file = f
	return nil
}

func (dw *dailyWriter) Close() error {
	dw.mu.Lock()
	defer dw.mu.Unlock()
	if dw.file != nil {
		return dw.file.Close()
	}
	return nil
}

// Setup configures the structured JSON logger with daily rotation.
func Setup(cfg LogConfig) (*slog.Logger, func(), error) {
	dir := expandHome(cfg.Dir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, nil, fmt.Errorf("create log dir %s: %w", dir, err)
	}

	// Cleanup old logs
	go cleanOldLogs(dir, cfg.MaxAgeDays)

	level := parseLevel(cfg.Level)

	dw, err := newDailyWriter(dir)
	if err != nil {
		return nil, nil, fmt.Errorf("create daily writer: %w", err)
	}

	var writer io.Writer = dw
	if level == slog.LevelDebug {
		writer = io.MultiWriter(os.Stderr, dw)
	}

	handler := slog.NewJSONHandler(writer, &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				a.Key = "time"
			}
			return a
		},
	})

	logger := slog.New(handler)
	slog.SetDefault(logger)

	return logger, func() { dw.Close() }, nil
}

// LogConfig is a subset of config.LogConfig for the log package.
type LogConfig struct {
	Level      string
	Dir        string
	MaxAgeDays int
}

// FromConfig converts config.LogConfig to log.LogConfig.
func FromConfig(level, dir string, maxAgeDays int) LogConfig {
	return LogConfig{Level: level, Dir: dir, MaxAgeDays: maxAgeDays}
}

// WithRequestID returns a context with a new request ID.
func WithRequestID(ctx context.Context) context.Context {
	return context.WithValue(ctx, requestIDKey, uuid.New().String())
}

// RequestIDFromContext extracts the request ID from context.
func RequestIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey).(string); ok {
		return id
	}
	return ""
}

func expandHome(path string) string {
	if len(path) == 0 || path[0] != '~' {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[1:])
}

func parseLevel(s string) slog.Level {
	switch s {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func cleanOldLogs(dir string, maxDays int) {
	cutoff := time.Now().AddDate(0, 0, -maxDays)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if len(name) < len("onellm-router-2006-01-02.log") {
			continue
		}
		dateStr := name[len("onellm-router-") : len("onellm-router-")+10]
		if t, err := time.Parse("2006-01-02", dateStr); err == nil && t.Before(cutoff) {
			os.Remove(filepath.Join(dir, name))
		}
	}
}
