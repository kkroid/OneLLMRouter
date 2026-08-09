package usage

import (
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Store appends usage records to UTC daily JSONL files.
type Store struct {
	mu      sync.Mutex
	baseDir string
	logger  *slog.Logger
	now     func() time.Time
}

// NewStore creates a usage store. An empty base directory uses
// ~/.onellm/usage. The optional clock is intended for tests.
func NewStore(baseDir string, logger *slog.Logger, clocks ...func() time.Time) *Store {
	if baseDir == "" {
		baseDir = defaultBaseDir()
	}
	now := time.Now
	if len(clocks) > 0 && clocks[0] != nil {
		now = clocks[0]
	}
	return &Store{
		baseDir: baseDir,
		logger:  logger,
		now:     now,
	}
}

// Write appends one record. Collection call sites own attempt deduplication.
func (s *Store) Write(record Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	record.Time = s.now().UTC()
	data, err := json.Marshal(record)
	if err == nil {
		err = os.MkdirAll(s.baseDir, 0755)
	}
	if err == nil {
		path := filepath.Join(s.baseDir, record.Time.Format("2006-01-02")+".jsonl")
		var file *os.File
		file, err = os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err == nil {
			line := append(data, '\n')
			var written int
			written, err = file.Write(line)
			if err == nil && written != len(line) {
				err = io.ErrShortWrite
			}
			if closeErr := file.Close(); err == nil {
				err = closeErr
			}
		}
	}
	if err != nil {
		s.logWriteError(record, err)
		return err
	}

	return nil
}

func (s *Store) logWriteError(record Record, err error) {
	if s.logger == nil {
		return
	}
	s.logger.Error("write usage record",
		"error", err,
		"request_id", record.RequestID,
		"upstream_attempt", record.UpstreamAttempt,
	)
}

func defaultBaseDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".onellm", "usage")
	}
	return filepath.Join(home, ".onellm", "usage")
}
