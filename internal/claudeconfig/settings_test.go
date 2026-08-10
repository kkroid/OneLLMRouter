package claudeconfig

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func testValues() Values {
	return Values{
		BaseURL: "http://localhost:45678/anthropic",
		Default: "alpha/default", Opus: "alpha/opus", Sonnet: "alpha/sonnet",
		Haiku: "alpha/haiku", Fable: "alpha/fable",
	}
}

func TestApplyPreservesUnknownFieldsAndRestoreExactBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	original := []byte("{\n  \"theme\": \"light\",\n  \"env\": {\"KEEP\": \"yes\", \"ANTHROPIC_MODEL\": \"old\"},\n  \"hooks\": {\"fake\": true}\n}\n")
	if err := os.WriteFile(path, original, 0o640); err != nil {
		t.Fatal(err)
	}

	applied, failure := Apply(path, testValues())
	if failure != nil {
		t.Fatalf("apply failure = %+v", failure)
	}
	if !applied.Changed || !applied.BackupCreated || !applied.AtomicReplaced || applied.SyncState != "current" {
		t.Fatalf("apply result = %+v", applied)
	}
	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range [][]byte{[]byte(`"theme": "light"`), []byte(`"KEEP": "yes"`), []byte(`"hooks"`), []byte(`"ANTHROPIC_AUTH_TOKEN": "x"`)} {
		if !bytes.Contains(updated, expected) {
			t.Fatalf("updated settings missing %s: %s", expected, updated)
		}
	}
	backup, err := os.ReadFile(path + ".bak")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(backup, original) {
		t.Fatal("backup did not preserve exact original bytes")
	}

	second, failure := Apply(path, testValues())
	if failure != nil || second.Changed || second.BackupCreated || second.AtomicReplaced {
		t.Fatalf("second apply = %+v, failure = %+v", second, failure)
	}
	backupAfter, _ := os.ReadFile(path + ".bak")
	if !bytes.Equal(backupAfter, original) {
		t.Fatal("idempotent apply rotated backup")
	}

	restored, failure := Restore(path, testValues())
	if failure != nil || !restored.Changed || !restored.AtomicReplaced || restored.SyncState != "different" {
		t.Fatalf("restore = %+v, failure = %+v", restored, failure)
	}
	restoredBytes, _ := os.ReadFile(path)
	if !bytes.Equal(restoredBytes, original) {
		t.Fatal("restore did not write exact backup bytes")
	}
}

func TestApplyAbsentCreatesSettingsWithoutBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new", "settings.json")
	result, failure := Apply(path, testValues())
	if failure != nil {
		t.Fatalf("apply failure = %+v", failure)
	}
	if !result.Changed || result.BackupCreated || result.BackupExists || result.SyncState != "current" {
		t.Fatalf("result = %+v", result)
	}
	if _, err := os.Stat(path + ".bak"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("backup stat error = %v", err)
	}
}

func TestInvalidSettingsAndBackupAreNeverApplied(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	invalid := []byte(`{"env":"secret-marker"}`)
	if err := os.WriteFile(path, invalid, 0o600); err != nil {
		t.Fatal(err)
	}
	result, failure := Apply(path, testValues())
	if failure == nil || failure.Code != "settings_invalid" || result.Changed {
		t.Fatalf("result = %+v, failure = %+v", result, failure)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(after, invalid) {
		t.Fatal("invalid settings were overwritten")
	}

	valid := []byte(`{"env":{}}`)
	if err := os.WriteFile(path, valid, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".bak", []byte(`[]`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, failure = Restore(path, testValues())
	if failure == nil || failure.Code != "backup_invalid" {
		t.Fatalf("restore failure = %+v", failure)
	}
	after, _ = os.ReadFile(path)
	if !bytes.Equal(after, valid) {
		t.Fatal("malformed backup changed settings")
	}
}

func TestSettingsReplacementFailureLeavesOriginalBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	original := []byte(`{"env":{"KEEP":"yes"}}`)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	originalRename := renameFile
	renameFile = func(oldPath, newPath string) error {
		if newPath == path {
			return errors.New("injected replacement failure")
		}
		return os.Rename(oldPath, newPath)
	}
	t.Cleanup(func() { renameFile = originalRename })

	result, failure := Apply(path, testValues())
	if failure == nil || failure.Code != "write_failed" || result.AtomicReplaced {
		t.Fatalf("result = %+v, failure = %+v", result, failure)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(after, original) {
		t.Fatal("failed atomic replacement changed settings")
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() != "settings.json" && entry.Name() != "settings.json.bak" {
			t.Fatalf("temporary file left behind: %s", entry.Name())
		}
	}
}

func TestBackupReplacementFailureLeavesSettingsAndBackupUnchanged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	original := []byte(`{"env":{"KEEP":"yes"}}`)
	oldBackup := []byte(`{"env":{"OLDER":"yes"}}`)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".bak", oldBackup, 0o600); err != nil {
		t.Fatal(err)
	}
	originalRename := renameFile
	renameFile = func(oldPath, newPath string) error {
		if newPath == path+".bak" {
			return errors.New("injected backup failure")
		}
		return os.Rename(oldPath, newPath)
	}
	t.Cleanup(func() { renameFile = originalRename })

	result, failure := Apply(path, testValues())
	if failure == nil || failure.Code != "write_failed" || result.Changed || result.BackupCreated {
		t.Fatalf("result = %+v, failure = %+v", result, failure)
	}
	after, _ := os.ReadFile(path)
	backupAfter, _ := os.ReadFile(path + ".bak")
	if !bytes.Equal(after, original) || !bytes.Equal(backupAfter, oldBackup) {
		t.Fatal("backup failure changed settings or existing backup")
	}
}

func TestStatusCountsDistinctConfiguredModels(t *testing.T) {
	values := testValues()
	values.Fable = values.Haiku
	result, failure := Status(filepath.Join(t.TempDir(), "missing.json"), values)
	if failure != nil || result.ParseState != "absent" || result.SyncState != "absent" || result.ConfiguredModelCount != 4 || result.ManagedKeyCount != 7 {
		t.Fatalf("result = %+v, failure = %+v", result, failure)
	}
}
