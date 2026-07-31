package upstream

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kkroid/onellm-router/internal/config"
	onellmLog "github.com/kkroid/onellm-router/internal/log"
)

type Mode uint8

const (
	Headers Mode = iota
	Buffered
)

type RequestFactory func(context.Context) (*http.Request, error)

type Metadata struct {
	RequestID string
	Provider  string
	Model     string
	Endpoint  string
}

type Options struct {
	Mode              Mode
	PerAttemptTimeout time.Duration
	SuccessBodyLimit  int64
	Sanitizer         *Sanitizer
}

type Result struct {
	Response *http.Response
	Body     []byte
	Attempts int
	Elapsed  time.Duration
}

type FailureKind string

const (
	FailureHTTP            FailureKind = "http"
	FailureTransport       FailureKind = "transport"
	FailureBodyRead        FailureKind = "body_read"
	FailureTimeout         FailureKind = "timeout"
	FailureProtocol        FailureKind = "protocol"
	FailureLocal           FailureKind = "local"
	FailureClientCancel    FailureKind = "client_cancel"
	FailureServiceShutdown FailureKind = "service_shutdown"
	FailureCanceled                    = FailureClientCancel
)

type Failure struct {
	StatusCode int
	Kind       FailureKind
	Summary    string
	Err        error
	Attempts   int
	Elapsed    time.Duration
}

type Executor struct {
	policy config.RetryConfig
	logger *slog.Logger
	now    func() time.Time
	wait   func(context.Context, time.Duration) error
	jitter func() float64
}

var (
	errAttemptTimeout  = errors.New("upstream attempt timeout")
	ErrServiceShutdown = errors.New("OneLLMRouter service shutdown")
)

func NewExecutor(policy config.RetryConfig, loggers ...*slog.Logger) *Executor {
	executor := &Executor{
		policy: policy,
		now:    time.Now,
		wait:   waitContext,
		jitter: rand.Float64,
	}
	if len(loggers) > 0 {
		executor.logger = loggers[0]
	}
	return executor
}

func (e *Executor) Do(
	ctx context.Context,
	client *http.Client,
	metadata Metadata,
	options Options,
	factory RequestFactory,
) (*Result, *Failure) {
	started := e.now()
	maxAttempts := e.policy.MaxAttempts
	if !e.policy.Enabled {
		maxAttempts = 1
	}
	inferenceClient := inferenceClient(client)
	var lastFailure *Failure

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := context.Cause(ctx); err != nil {
			return e.completeFailure(ctx, metadata, maxAttempts, options.Sanitizer, e.contextFailure(started, attempt-1, err))
		}
		if attempt > 1 && e.elapsed(started) >= time.Duration(e.policy.MaxElapsed) {
			return e.completeFailure(ctx, metadata, maxAttempts, options.Sanitizer, lastFailure)
		}

		attemptContext, stopTimeout, release := e.startAttempt(ctx, started, options.PerAttemptTimeout)

		request, err := factory(attemptContext)
		if err != nil {
			release()
			return e.completeFailure(ctx, metadata, maxAttempts, options.Sanitizer, &Failure{
				Kind:     FailureLocal,
				Err:      err,
				Attempts: attempt,
				Elapsed:  e.elapsed(started),
			})
		}
		if request == nil {
			release()
			return e.completeFailure(ctx, metadata, maxAttempts, options.Sanitizer, &Failure{
				Kind:     FailureLocal,
				Err:      errors.New("request factory returned a nil request"),
				Attempts: attempt,
				Elapsed:  e.elapsed(started),
			})
		}

		response, requestErr := inferenceClient.Do(request)
		if requestErr == nil && response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices {
			if options.Mode == Buffered {
				result, failure := e.readBuffered(started, attempt, attemptContext, response, options.SuccessBodyLimit, options.Sanitizer)
				stopTimeout()
				if failure == nil {
					if cause := context.Cause(attemptContext); cause != nil {
						failure = e.contextFailure(started, attempt, cause)
					}
				}
				release()
				if failure == nil {
					return e.completeSuccess(ctx, metadata, maxAttempts, result, lastFailure)
				}
				if failure.Kind == FailureProtocol {
					e.logAttemptFailure(metadata, maxAttempts, failure, 0, options.Sanitizer)
					return e.completeFailure(ctx, metadata, maxAttempts, options.Sanitizer, failure)
				}
				if isCancellationFailure(failure) {
					return e.completeFailure(ctx, metadata, maxAttempts, options.Sanitizer, failure)
				}
				lastFailure = failure
				response = nil
			} else {
				stopTimeout()
				if cause := context.Cause(attemptContext); cause != nil {
					if response.Body != nil {
						_ = response.Body.Close()
					}
					release()
					lastFailure = e.contextFailure(started, attempt, cause)
					response = nil
					if isCancellationFailure(lastFailure) {
						return e.completeFailure(ctx, metadata, maxAttempts, options.Sanitizer, lastFailure)
					}
				} else {
					response.Body = &cancelOnClose{
						ReadCloser: response.Body,
						cancel:     release,
					}
					return e.completeSuccess(ctx, metadata, maxAttempts, &Result{
						Response: response,
						Attempts: attempt,
						Elapsed:  e.elapsed(started),
					}, lastFailure)
				}
			}
		} else {
			var failureBody []byte
			if response != nil && response.Body != nil {
				failureBody, _ = io.ReadAll(io.LimitReader(response.Body, 4097))
				_ = response.Body.Close()
			}
			lastFailure = e.attemptFailure(started, attempt, attemptContext, response, requestErr, failureBody, options.Sanitizer)
			release()
			if isCancellationFailure(lastFailure) {
				return e.completeFailure(ctx, metadata, maxAttempts, options.Sanitizer, lastFailure)
			}
		}

		if attempt == maxAttempts {
			e.logAttemptFailure(metadata, maxAttempts, lastFailure, 0, options.Sanitizer)
			return e.completeFailure(ctx, metadata, maxAttempts, options.Sanitizer, lastFailure)
		}

		delay := e.retryDelay(attempt, response)
		remaining := time.Duration(e.policy.MaxElapsed) - e.elapsed(started)
		if remaining <= 0 || delay >= remaining {
			e.logAttemptFailure(metadata, maxAttempts, lastFailure, 0, options.Sanitizer)
			return e.completeFailure(ctx, metadata, maxAttempts, options.Sanitizer, lastFailure)
		}
		e.logAttemptFailure(metadata, maxAttempts, lastFailure, delay, options.Sanitizer)
		if err := e.wait(ctx, delay); err != nil {
			cause := context.Cause(ctx)
			if cause == nil {
				cause = err
			}
			return e.completeFailure(ctx, metadata, maxAttempts, options.Sanitizer, e.contextFailure(started, attempt, cause))
		}
		if e.elapsed(started) >= time.Duration(e.policy.MaxElapsed) {
			return e.completeFailure(ctx, metadata, maxAttempts, options.Sanitizer, lastFailure)
		}
	}

	panic("unreachable")
}

func (e *Executor) completeSuccess(
	ctx context.Context,
	metadata Metadata,
	maxAttempts int,
	result *Result,
	lastFailure *Failure,
) (*Result, *Failure) {
	e.recordRequestMeta(ctx, result.Attempts, result.Elapsed, lastFailure)
	if e.logger != nil && result.Attempts > 1 {
		attrs := e.baseLogAttrs(metadata)
		attrs = append(attrs,
			"attempts", result.Attempts,
			"max_attempts", maxAttempts,
			"elapsed_ms", result.Elapsed.Milliseconds(),
		)
		e.logger.Info("upstream recovered", attrs...)
	}
	return result, nil
}

func (e *Executor) completeFailure(
	ctx context.Context,
	metadata Metadata,
	maxAttempts int,
	sanitizer *Sanitizer,
	failure *Failure,
) (*Result, *Failure) {
	e.recordRequestMeta(ctx, failure.Attempts, failure.Elapsed, failure)
	if e.logger == nil || failure.Kind == FailureLocal {
		return nil, failure
	}
	attrs := e.baseLogAttrs(metadata)
	attrs = append(attrs,
		"attempts", failure.Attempts,
		"max_attempts", maxAttempts,
		"failure_kind", string(failure.Kind),
		"error", failureLogError(failure, sanitizer),
		"elapsed_ms", failure.Elapsed.Milliseconds(),
	)
	if failure.StatusCode != 0 {
		attrs = append(attrs, "status", failure.StatusCode)
	}
	if failure.Kind == FailureClientCancel || failure.Kind == FailureServiceShutdown {
		e.logger.Info("upstream retry canceled", attrs...)
	} else {
		e.logger.Error("upstream retry exhausted", attrs...)
	}
	return nil, failure
}

func isCancellationFailure(failure *Failure) bool {
	return failure.Kind == FailureClientCancel || failure.Kind == FailureServiceShutdown
}

func (e *Executor) logAttemptFailure(metadata Metadata, maxAttempts int, failure *Failure, delay time.Duration, sanitizer *Sanitizer) {
	if e.logger == nil || failure == nil {
		return
	}
	attrs := e.baseLogAttrs(metadata)
	attrs = append(attrs,
		"attempt", failure.Attempts,
		"max_attempts", maxAttempts,
		"failure_kind", string(failure.Kind),
		"error", failureLogError(failure, sanitizer),
		"delay_ms", delay.Milliseconds(),
		"elapsed_ms", failure.Elapsed.Milliseconds(),
	)
	if failure.StatusCode != 0 {
		attrs = append(attrs, "status", failure.StatusCode)
	}
	e.logger.Warn("upstream attempt failed", attrs...)
}

func (e *Executor) baseLogAttrs(metadata Metadata) []any {
	return []any{
		"request_id", metadata.RequestID,
		"provider", metadata.Provider,
		"model", metadata.Model,
		"endpoint", metadata.Endpoint,
	}
}

func (e *Executor) recordRequestMeta(ctx context.Context, attempts int, elapsed time.Duration, lastFailure *Failure) {
	meta := onellmLog.RequestMetaFromContext(ctx)
	meta.UpstreamAttempts = attempts
	meta.RetryElapsedMs = elapsed.Milliseconds()
	if lastFailure != nil {
		meta.LastUpstreamStatus = lastFailure.StatusCode
		meta.LastFailureKind = string(lastFailure.Kind)
	}
}

func failureLogError(failure *Failure, sanitizer *Sanitizer) string {
	if failure.Summary != "" {
		return failure.Summary
	}
	if failure.Err != nil {
		return sanitizeWith(sanitizer, []byte(failure.Err.Error()))
	}
	return string(failure.Kind)
}

func (e *Executor) attemptFailure(
	started time.Time,
	attempt int,
	attemptContext context.Context,
	response *http.Response,
	err error,
	body []byte,
	sanitizer *Sanitizer,
) *Failure {
	if cause := context.Cause(attemptContext); cause != nil {
		knownHTTPFailure := response != nil &&
			(response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices)
		if !errors.Is(cause, errAttemptTimeout) || !knownHTTPFailure {
			return e.contextFailure(started, attempt, cause)
		}
	}
	failure := &Failure{Attempts: attempt, Elapsed: e.elapsed(started)}
	if response != nil && (response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices) {
		failure.Kind = FailureHTTP
		failure.StatusCode = response.StatusCode
		failure.Summary = sanitizeWith(sanitizer, body)
		failure.Err = fmt.Errorf("upstream returned HTTP %d", response.StatusCode)
		return failure
	}
	if err != nil {
		summary := sanitizeWith(sanitizer, []byte(err.Error()))
		if isTimeoutError(err) {
			failure.StatusCode = http.StatusGatewayTimeout
			failure.Kind = FailureTimeout
			failure.Summary = summary
			failure.Err = errors.New(summary)
			return failure
		}
		failure.Kind = FailureTransport
		failure.Summary = summary
		failure.Err = errors.New(summary)
		return failure
	}
	panic("attempt failure has neither response nor error")
}

func (e *Executor) readBuffered(
	started time.Time,
	attempt int,
	attemptContext context.Context,
	response *http.Response,
	limit int64,
	sanitizer *Sanitizer,
) (*Result, *Failure) {
	reader := io.Reader(response.Body)
	if limit > 0 {
		reader = io.LimitReader(response.Body, limit+1)
	}
	body, err := io.ReadAll(reader)
	_ = response.Body.Close()
	if cause := context.Cause(attemptContext); cause != nil {
		return nil, e.contextFailure(started, attempt, cause)
	}
	if err != nil {
		summary := sanitizeWith(sanitizer, []byte(err.Error()))
		if isTimeoutError(err) {
			return nil, &Failure{
				StatusCode: http.StatusGatewayTimeout,
				Kind:       FailureTimeout,
				Summary:    summary,
				Err:        errors.New(summary),
				Attempts:   attempt,
				Elapsed:    e.elapsed(started),
			}
		}
		return nil, &Failure{
			StatusCode: http.StatusBadGateway,
			Kind:       FailureBodyRead,
			Summary:    summary,
			Err:        errors.New(summary),
			Attempts:   attempt,
			Elapsed:    e.elapsed(started),
		}
	}
	if limit > 0 && int64(len(body)) > limit {
		return nil, &Failure{
			StatusCode: http.StatusBadGateway,
			Kind:       FailureProtocol,
			Err:        fmt.Errorf("successful upstream response exceeds %d-byte limit", limit),
			Attempts:   attempt,
			Elapsed:    e.elapsed(started),
		}
	}
	return &Result{
		Response: response,
		Body:     body,
		Attempts: attempt,
		Elapsed:  e.elapsed(started),
	}, nil
}

func isTimeoutError(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var timeout net.Error
	return errors.As(err, &timeout) && timeout.Timeout()
}

func (e *Executor) startAttempt(parent context.Context, started time.Time, perAttemptTimeout time.Duration) (context.Context, func(), func()) {
	remaining := time.Duration(e.policy.MaxElapsed) - e.elapsed(started)
	timeout := perAttemptTimeout
	if timeout <= 0 || timeout > remaining {
		timeout = remaining
	}
	attemptContext, cancel := context.WithCancelCause(parent)
	timerFired := make(chan struct{})
	timer := time.AfterFunc(timeout, func() {
		cancel(errAttemptTimeout)
		close(timerFired)
	})
	var stopOnce sync.Once
	stopped := make(chan struct{})
	stopTimeout := func() {
		stopOnce.Do(func() {
			if !timer.Stop() {
				<-timerFired
			}
			close(stopped)
		})
		<-stopped
	}
	release := func() {
		stopTimeout()
		cancel(nil)
	}
	return attemptContext, stopTimeout, release
}

func (e *Executor) contextFailure(started time.Time, attempt int, cause error) *Failure {
	if errors.Is(cause, errAttemptTimeout) {
		return &Failure{
			StatusCode: http.StatusGatewayTimeout,
			Kind:       FailureTimeout,
			Err:        cause,
			Attempts:   attempt,
			Elapsed:    e.elapsed(started),
		}
	}
	if errors.Is(cause, ErrServiceShutdown) {
		return &Failure{
			Kind:     FailureServiceShutdown,
			Err:      cause,
			Attempts: attempt,
			Elapsed:  e.elapsed(started),
		}
	}
	return e.canceledFailure(started, attempt, cause)
}

func (e *Executor) canceledFailure(started time.Time, attempts int, err error) *Failure {
	return &Failure{
		Kind:     FailureCanceled,
		Err:      err,
		Attempts: attempts,
		Elapsed:  e.elapsed(started),
	}
}

func (e *Executor) elapsed(started time.Time) time.Duration {
	elapsed := e.now().Sub(started)
	if elapsed < 0 {
		return 0
	}
	return elapsed
}

func (e *Executor) retryDelay(attempt int, response *http.Response) time.Duration {
	maxDelay := time.Duration(e.policy.MaxDelay)
	if e.policy.HonorRetryAfter && response != nil {
		if delay, ok := parseRetryAfter(response.Header.Get("Retry-After"), e.now()); ok {
			if delay > maxDelay {
				return maxDelay
			}
			return delay
		}
	}

	delay := time.Duration(e.policy.InitialDelay)
	for step := 1; step < attempt && delay < maxDelay; step++ {
		if delay > maxDelay/2 {
			delay = maxDelay
			break
		}
		delay *= 2
	}
	if delay > maxDelay {
		delay = maxDelay
	}

	factor := 1 + e.policy.Jitter*(2*e.jitter()-1)
	delay = time.Duration(float64(delay) * factor)
	if delay < 0 {
		return 0
	}
	if delay > maxDelay {
		return maxDelay
	}
	return delay
}

func parseRetryAfter(value string, now time.Time) (time.Duration, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		if seconds < 0 || seconds > int64((1<<63-1)/time.Second) {
			return 0, false
		}
		return time.Duration(seconds) * time.Second, true
	}
	retryAt, err := http.ParseTime(value)
	if err != nil {
		return 0, false
	}
	delay := retryAt.Sub(now)
	if delay <= 0 {
		return 0, false
	}
	return delay, true
}

func inferenceClient(client *http.Client) *http.Client {
	if client == nil {
		client = http.DefaultClient
	}
	copy := *client
	transport := copy.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	if _, standardTransport := transport.(*http.Transport); !standardTransport {
		copy.Transport = closeErrorResponseTransport{RoundTripper: transport}
	}
	copy.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &copy
}

type closeErrorResponseTransport struct {
	http.RoundTripper
}

func (transport closeErrorResponseTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := transport.RoundTripper.RoundTrip(request)
	if err == nil || response == nil || response.Body == nil {
		return response, err
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxFailureSummaryBytes+1))
	_ = response.Body.Close()
	return nil, err
}

func waitContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return context.Cause(ctx)
	}
}

type cancelOnClose struct {
	io.ReadCloser
	cancel func()
	once   sync.Once
	err    error
}

func (body *cancelOnClose) Close() error {
	body.once.Do(func() {
		body.err = body.ReadCloser.Close()
		body.cancel()
	})
	return body.err
}
