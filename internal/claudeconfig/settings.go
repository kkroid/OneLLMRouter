package claudeconfig

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

const ManagedKeyCount = 7

type Values struct {
	BaseURL string
	Default string
	Opus    string
	Sonnet  string
	Haiku   string
	Fable   string
}

type Result struct {
	SettingsPath         string `json:"settings_path"`
	BackupPath           string `json:"backup_path"`
	Exists               bool   `json:"exists"`
	BackupExists         bool   `json:"backup_exists"`
	ParseState           string `json:"parse_state"`
	SyncState            string `json:"sync_state"`
	ManagedKeyCount      int    `json:"managed_key_count"`
	ConfiguredModelCount int    `json:"configured_model_count"`
	Changed              bool   `json:"changed"`
	BackupCreated        bool   `json:"backup_created"`
	AtomicReplaced       bool   `json:"atomic_replaced"`
}

type Failure struct {
	Code    string
	Message string
	Path    string
}

type document map[string]json.RawMessage

var renameFile = os.Rename

func Status(path string, values Values) (Result, *Failure) {
	result := newResult(path, values)
	settings, state, _ := readDocument(path, "settings")
	result.Exists = state != "absent"
	result.ParseState = state
	result.BackupExists = fileExists(result.BackupPath)
	result.SyncState = syncState(state, settings, values)
	return result, nil
}

func Apply(path string, values Values) (Result, *Failure) {
	result, failure := Status(path, values)
	if failure != nil {
		return result, failure
	}
	if result.ParseState == "invalid" {
		return result, settingsFailure("settings_invalid", path)
	}
	if result.ParseState == "unreadable" {
		return result, settingsFailure("settings_unreadable", path)
	}
	if result.SyncState == "current" {
		return result, nil
	}

	var original []byte
	mode := fs.FileMode(0o600)
	settings := document{}
	if result.Exists {
		var err error
		original, err = os.ReadFile(path)
		if err != nil {
			result.ParseState = "unreadable"
			result.SyncState = "unreadable"
			return result, settingsFailure("settings_unreadable", path)
		}
		if err := json.Unmarshal(original, &settings); err != nil {
			return result, settingsFailure("settings_invalid", path)
		}
		if info, err := os.Stat(path); err == nil {
			mode = info.Mode().Perm()
		}
	}

	merge(settings, values)
	updated, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return result, settingsFailure("write_failed", path)
	}
	updated = append(updated, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return result, settingsFailure("write_failed", path)
	}
	stagedSettings, err := stage(path, updated, mode)
	if err != nil {
		return result, settingsFailure("write_failed", path)
	}
	defer os.Remove(stagedSettings)

	if result.Exists {
		stagedBackup, err := stage(result.BackupPath, original, 0o600)
		if err != nil {
			return result, settingsFailure("write_failed", result.BackupPath)
		}
		defer os.Remove(stagedBackup)
		if err := renameFile(stagedBackup, result.BackupPath); err != nil {
			return result, settingsFailure("write_failed", result.BackupPath)
		}
		result.BackupCreated = true
		result.BackupExists = true
		syncDirectory(filepath.Dir(path))
	}
	if err := renameFile(stagedSettings, path); err != nil {
		return result, settingsFailure("write_failed", path)
	}
	syncDirectory(filepath.Dir(path))
	result.Exists = true
	result.ParseState = "valid"
	result.SyncState = "current"
	result.Changed = true
	result.AtomicReplaced = true
	return result, nil
}

func Restore(path string, values Values) (Result, *Failure) {
	result, _ := Status(path, values)
	backup, state, failure := readDocument(result.BackupPath, "backup")
	if failure != nil {
		return result, failure
	}
	if state == "absent" {
		return result, backupFailure("backup_missing", result.BackupPath)
	}
	if state == "invalid" {
		return result, backupFailure("backup_invalid", result.BackupPath)
	}
	backupBytes, err := os.ReadFile(result.BackupPath)
	if err != nil {
		return result, backupFailure("backup_unreadable", result.BackupPath)
	}
	mode := fs.FileMode(0o600)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	staged, err := stage(path, backupBytes, mode)
	if err != nil {
		return result, settingsFailure("write_failed", path)
	}
	defer os.Remove(staged)
	if err := renameFile(staged, path); err != nil {
		return result, settingsFailure("write_failed", path)
	}
	syncDirectory(filepath.Dir(path))
	result.Exists = true
	result.BackupExists = true
	result.ParseState = "valid"
	result.SyncState = syncState("valid", backup, values)
	result.Changed = true
	result.AtomicReplaced = true
	return result, nil
}

func newResult(path string, values Values) Result {
	return Result{
		SettingsPath:         path,
		BackupPath:           path + ".bak",
		ManagedKeyCount:      ManagedKeyCount,
		ConfiguredModelCount: configuredModelCount(values),
		ParseState:           "absent",
		SyncState:            "absent",
	}
}

func readDocument(path, kind string) (document, string, *Failure) {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, "absent", nil
	}
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		if kind == "backup" {
			return nil, "unreadable", backupFailure("backup_unreadable", path)
		}
		return nil, "unreadable", settingsFailure("settings_unreadable", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if kind == "backup" {
			return nil, "unreadable", backupFailure("backup_unreadable", path)
		}
		return nil, "unreadable", settingsFailure("settings_unreadable", path)
	}
	var settings document
	if err := json.Unmarshal(data, &settings); err != nil || settings == nil {
		return nil, "invalid", nil
	}
	if raw, exists := settings["env"]; exists {
		var env map[string]json.RawMessage
		if err := json.Unmarshal(raw, &env); err != nil || env == nil {
			return nil, "invalid", nil
		}
	}
	return settings, "valid", nil
}

func merge(settings document, values Values) {
	env := map[string]json.RawMessage{}
	if raw, exists := settings["env"]; exists {
		_ = json.Unmarshal(raw, &env)
	}
	for key, value := range managed(values) {
		encoded, _ := json.Marshal(value)
		env[key] = encoded
	}
	settings["env"], _ = json.Marshal(env)
}

func syncState(parseState string, settings document, values Values) string {
	if parseState != "valid" {
		return parseState
	}
	var env map[string]json.RawMessage
	_ = json.Unmarshal(settings["env"], &env)
	for key, expected := range managed(values) {
		var actual string
		if err := json.Unmarshal(env[key], &actual); err != nil || actual != expected {
			return "different"
		}
	}
	return "current"
}

func managed(values Values) map[string]string {
	return map[string]string{
		"ANTHROPIC_BASE_URL":             values.BaseURL,
		"ANTHROPIC_AUTH_TOKEN":           "x",
		"ANTHROPIC_MODEL":                values.Default,
		"ANTHROPIC_DEFAULT_OPUS_MODEL":   values.Opus,
		"ANTHROPIC_DEFAULT_SONNET_MODEL": values.Sonnet,
		"ANTHROPIC_DEFAULT_HAIKU_MODEL":  values.Haiku,
		"ANTHROPIC_DEFAULT_FABLE_MODEL":  values.Fable,
	}
}

func configuredModelCount(values Values) int {
	models := map[string]struct{}{}
	for _, model := range []string{values.Default, values.Opus, values.Sonnet, values.Haiku, values.Fable} {
		if model != "" {
			models[model] = struct{}{}
		}
	}
	return len(models)
}

func stage(destination string, data []byte, mode fs.FileMode) (string, error) {
	file, err := os.CreateTemp(filepath.Dir(destination), "."+filepath.Base(destination)+"-*")
	if err != nil {
		return "", err
	}
	path := file.Name()
	failed := true
	defer func() {
		file.Close()
		if failed {
			os.Remove(path)
		}
	}()
	if err := file.Chmod(mode); err != nil {
		return "", err
	}
	if _, err := file.Write(data); err != nil {
		return "", err
	}
	if err := file.Sync(); err != nil {
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	failed = false
	return path, nil
}

func syncDirectory(path string) {
	directory, err := os.Open(path)
	if err == nil {
		_ = directory.Sync()
		_ = directory.Close()
	}
}

func fileExists(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular()
}

func settingsFailure(code, path string) *Failure {
	message := map[string]string{
		"settings_invalid":    "Claude settings are not a valid JSON object with an object-valued env member",
		"settings_unreadable": "Claude settings could not be read safely",
		"write_failed":        "Claude settings could not be replaced safely",
	}[code]
	return &Failure{Code: code, Message: message, Path: path}
}

func backupFailure(code, path string) *Failure {
	message := map[string]string{
		"backup_missing":    "Claude settings backup does not exist",
		"backup_invalid":    "Claude settings backup is not a valid JSON object with an object-valued env member",
		"backup_unreadable": "Claude settings backup could not be read safely",
	}[code]
	return &Failure{Code: code, Message: message, Path: path}
}
