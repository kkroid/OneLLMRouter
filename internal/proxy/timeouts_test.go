package proxy

import (
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func TestDurationFromEnvUsesMilliseconds(t *testing.T) {
	t.Setenv("ONELLM_TEST_TIMEOUT_MS", "125")

	if got := durationFromEnv("ONELLM_TEST_TIMEOUT_MS", time.Second); got != 125*time.Millisecond {
		t.Fatalf("duration = %v, want 125ms", got)
	}
}

func TestDurationFromEnvKeepsDefaultForInvalidValues(t *testing.T) {
	for _, value := range []string{"", "bad", "0", "-1"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("ONELLM_TEST_TIMEOUT_MS", value)
			if got := durationFromEnv("ONELLM_TEST_TIMEOUT_MS", time.Second); got != time.Second {
				t.Fatalf("duration = %v, want default", got)
			}
		})
	}
}

func TestStreamLinesTimesOutBeforeFirstData(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()

	err := streamLines(reader, 20*time.Millisecond, time.Second, func(string) error { return nil })
	if !errors.Is(err, errStreamFirstEventTimeout) {
		t.Fatalf("error = %v, want first-event timeout", err)
	}
}

func TestStreamLinesTimesOutWhenIdleAfterData(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	go func() {
		io.WriteString(writer, "event: message_start\n")
	}()

	var lines []string
	err := streamLines(reader, time.Second, 20*time.Millisecond, func(line string) error {
		lines = append(lines, line)
		return nil
	})
	if !errors.Is(err, errStreamIdleTimeout) {
		t.Fatalf("error = %v, want idle timeout", err)
	}
	if len(lines) != 1 || lines[0] != "event: message_start\n" {
		t.Fatalf("lines = %#v", lines)
	}
}

func TestStreamLinesReturnsCleanlyAtEOF(t *testing.T) {
	var lines []string
	err := streamLines(strings.NewReader("data: one\n\ndata: two\n\n"), time.Second, time.Second, func(line string) error {
		lines = append(lines, line)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(lines, "") != "data: one\n\ndata: two\n\n" {
		t.Fatalf("lines = %#v", lines)
	}
}
