package proxy

import (
	"bufio"
	"errors"
	"io"
	"os"
	"strconv"
	"time"
)

var (
	errStreamFirstEventTimeout = errors.New("timeout waiting for first upstream stream event")
	errStreamIdleTimeout       = errors.New("upstream stream idle timeout")
)

func durationFromEnv(name string, defaultValue time.Duration) time.Duration {
	value, ok := os.LookupEnv(name)
	if !ok {
		return defaultValue
	}
	milliseconds, err := strconv.ParseInt(value, 10, 64)
	if err != nil || milliseconds <= 0 {
		return defaultValue
	}
	return time.Duration(milliseconds) * time.Millisecond
}

func externalRequestTimeout() time.Duration {
	return durationFromEnv("ONELLM_EXTERNAL_REQUEST_TIMEOUT_MS", 60*time.Second)
}

func externalStreamTimeout() time.Duration {
	return durationFromEnv("ONELLM_EXTERNAL_STREAM_TIMEOUT_MS", 300*time.Second)
}

func openAIRequestTimeout() time.Duration {
	return durationFromEnv("ONELLM_OPENAI_REQUEST_TIMEOUT_MS", 2*time.Minute)
}

func streamFirstEventTimeout() time.Duration {
	return durationFromEnv("ONELLM_STREAM_FIRST_EVENT_TIMEOUT_MS", 300*time.Second)
}

func streamIdleTimeout() time.Duration {
	return durationFromEnv("ONELLM_STREAM_IDLE_TIMEOUT_MS", 300*time.Second)
}

func copilotRequestTimeout() time.Duration {
	return durationFromEnv("ONELLM_COPILOT_REQUEST_TIMEOUT_MS", 60*time.Second)
}

func copilotStreamTimeout() time.Duration {
	return durationFromEnv("ONELLM_COPILOT_STREAM_TIMEOUT_MS", 300*time.Second)
}

type streamLineResult struct {
	line string
	err  error
}

func streamLines(reader io.Reader, firstEventTimeout, idleTimeout time.Duration, handleLine func(string) error) error {
	results := make(chan streamLineResult)
	done := make(chan struct{})
	defer close(done)

	go func() {
		buffered := bufio.NewReader(reader)
		for {
			line, err := buffered.ReadString('\n')
			if line != "" {
				select {
				case results <- streamLineResult{line: line}:
				case <-done:
					return
				}
			}
			if err != nil {
				select {
				case results <- streamLineResult{err: err}:
				case <-done:
				}
				return
			}
		}
	}()

	timer := time.NewTimer(firstEventTimeout)
	defer timer.Stop()
	receivedData := false
	for {
		select {
		case result := <-results:
			if result.line != "" {
				if err := handleLine(result.line); err != nil {
					return err
				}
				receivedData = true
				resetTimer(timer, idleTimeout)
			}
			if result.err != nil {
				if errors.Is(result.err, io.EOF) {
					return nil
				}
				return result.err
			}
		case <-timer.C:
			if receivedData {
				return errStreamIdleTimeout
			}
			return errStreamFirstEventTimeout
		}
	}
}

func resetTimer(timer *time.Timer, duration time.Duration) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(duration)
}
