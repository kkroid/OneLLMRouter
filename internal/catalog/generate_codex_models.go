//go:build ignore

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const sourceURL = "https://raw.githubusercontent.com/openai/codex/rust-v0.144.6/codex-rs/models-manager/models.json"

func main() {
	source, closeSource := openSource()
	defer closeSource()

	var upstream struct {
		Models []json.RawMessage `json:"models"`
	}
	if err := json.NewDecoder(io.LimitReader(source, 8<<20)).Decode(&upstream); err != nil {
		panic(err)
	}

	wanted := map[string]bool{
		"gpt-5.5":       true,
		"gpt-5.6-sol":   true,
		"gpt-5.6-terra": true,
		"gpt-5.6-luna":  true,
	}
	selected := make([]json.RawMessage, 0, len(wanted))
	for _, raw := range upstream.Models {
		var identity struct {
			Slug string `json:"slug"`
		}
		if err := json.Unmarshal(raw, &identity); err != nil {
			panic(err)
		}
		if wanted[identity.Slug] {
			selected = append(selected, raw)
			delete(wanted, identity.Slug)
		}
	}
	if len(wanted) != 0 {
		panic(fmt.Sprintf("missing Codex models: %v", wanted))
	}

	document := struct {
		Source string            `json:"source"`
		Models []json.RawMessage `json:"models"`
	}{Source: sourceURL, Models: selected}
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		panic(err)
	}
	data = append(data, '\n')
	if err := os.WriteFile("codex_models_0.144.6.json", data, 0o644); err != nil {
		panic(err)
	}
}

func openSource() (io.Reader, func()) {
	if path := os.Getenv("CODEX_MODELS_SOURCE_FILE"); path != "" {
		file, err := os.Open(path)
		if err != nil {
			panic(err)
		}
		return file, func() { _ = file.Close() }
	}

	client := &http.Client{Timeout: 30 * time.Second}
	response, err := client.Get(sourceURL)
	if err != nil {
		panic(err)
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		panic(fmt.Sprintf("download Codex models: status %d", response.StatusCode))
	}
	return response.Body, func() { _ = response.Body.Close() }
}
