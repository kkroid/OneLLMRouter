package upstream

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kkroid/onellm-router/internal/config"
	onellmLog "github.com/kkroid/onellm-router/internal/log"
)

func TestRetryRebuildsRequestUntilSuccess(t *testing.T) {
	var factories atomic.Int32
	var calls atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		call := calls.Add(1)
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		if string(body) != "same body" {
			t.Fatalf("request body = %q", body)
		}
		status := http.StatusBadGateway
		if call == 3 {
			status = http.StatusOK
		}
		return testResponse(status), nil
	})}
	executor, _ := newTestExecutor(retryPolicy())

	result, failure := executor.Do(context.Background(), client, Metadata{}, Options{Mode: Headers}, func(ctx context.Context) (*http.Request, error) {
		factories.Add(1)
		return http.NewRequestWithContext(ctx, http.MethodPost, "https://example.test/v1/messages", strings.NewReader("same body"))
	})

	if failure != nil {
		t.Fatalf("Do() failure = %+v", failure)
	}
	if result == nil || result.Response.StatusCode != http.StatusOK {
		t.Fatalf("Do() result = %+v, want 200", result)
	}
	result.Response.Body.Close()
	if factories.Load() != 3 {
		t.Fatalf("factory calls = %d, want 3", factories.Load())
	}
}

func TestRetryDisabledCallsOnceWithoutWaiting(t *testing.T) {
	policy := retryPolicy()
	policy.Enabled = false
	executor, waits := newTestExecutor(policy)
	var calls atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return testResponse(http.StatusBadGateway), nil
	})}

	_, failure := executor.Do(context.Background(), client, Metadata{}, Options{Mode: Headers}, testRequestFactory())

	if failure == nil || failure.Attempts != 1 || failure.RetryEligible || calls.Load() != 1 || len(*waits) != 0 {
		t.Fatalf("failure = %+v, calls = %d, waits = %v", failure, calls.Load(), *waits)
	}
}

func TestRetryBackoffJitterCapAndLastAttempt(t *testing.T) {
	policy := retryPolicy()
	policy.MaxAttempts = 5
	policy.MaxDelay = config.Duration(3 * time.Second)
	executor, waits := newTestExecutor(policy)
	executor.jitter = func() float64 { return 0.5 }

	_, failure := executor.Do(context.Background(), failingClient(http.StatusBadGateway, nil), Metadata{}, Options{Mode: Headers}, testRequestFactory())

	if failure == nil || failure.Attempts != 5 {
		t.Fatalf("failure = %+v, want five attempts", failure)
	}
	assertDurations(t, *waits, time.Second, 2*time.Second, 3*time.Second, 3*time.Second)
}

func TestRetryJitterUsesConfiguredRange(t *testing.T) {
	policy := retryPolicy()
	policy.MaxAttempts = 2
	executor, waits := newTestExecutor(policy)
	executor.jitter = func() float64 { return 1 }

	executor.Do(context.Background(), failingClient(http.StatusBadGateway, nil), Metadata{}, Options{Mode: Headers}, testRequestFactory())

	assertDurations(t, *waits, 1200*time.Millisecond)
}

func TestRetryAfterPolicy(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		honor    bool
		maxDelay time.Duration
		want     time.Duration
	}{
		{name: "seconds capped", value: "10", honor: true, maxDelay: 3 * time.Second, want: 3 * time.Second},
		{name: "ignored", value: "10", honor: false, maxDelay: 30 * time.Second, want: time.Second},
		{name: "negative", value: "-1", honor: true, maxDelay: 30 * time.Second, want: time.Second},
		{name: "invalid", value: "not-a-date", honor: true, maxDelay: 30 * time.Second, want: time.Second},
		{name: "overflow", value: "999999999999999999999999", honor: true, maxDelay: 30 * time.Second, want: time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy := retryPolicy()
			policy.MaxAttempts = 2
			policy.MaxDelay = config.Duration(tt.maxDelay)
			policy.HonorRetryAfter = tt.honor
			executor, waits := newTestExecutor(policy)
			header := http.Header{"Retry-After": []string{tt.value}}
			executor.Do(context.Background(), failingClient(http.StatusTooManyRequests, header), Metadata{}, Options{Mode: Headers}, testRequestFactory())
			assertDurations(t, *waits, tt.want)
		})
	}
}

func TestRetryAfterDateUsesInjectedClock(t *testing.T) {
	policy := retryPolicy()
	policy.MaxAttempts = 2
	executor, waits := newTestExecutor(policy)
	now := executor.now()
	header := http.Header{"Retry-After": []string{now.Add(4 * time.Second).Format(http.TimeFormat)}}

	executor.Do(context.Background(), failingClient(http.StatusTooManyRequests, header), Metadata{}, Options{Mode: Headers}, testRequestFactory())

	assertDurations(t, *waits, 4*time.Second)
}

func TestRetryDoesNotWaitWithoutBudgetForNextAttempt(t *testing.T) {
	policy := retryPolicy()
	policy.MaxElapsed = config.Duration(time.Second)
	executor, waits := newTestExecutor(policy)
	var calls atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return testResponse(http.StatusBadGateway), nil
	})}

	_, failure := executor.Do(context.Background(), client, Metadata{}, Options{Mode: Headers}, testRequestFactory())

	if failure == nil || failure.Attempts != 1 || calls.Load() != 1 || len(*waits) != 0 {
		t.Fatalf("failure = %+v, calls = %d, waits = %v", failure, calls.Load(), *waits)
	}
}

func TestRetryDoesNotAttemptAfterWaitOvershootsBudget(t *testing.T) {
	policy := retryPolicy()
	policy.MaxElapsed = config.Duration(2 * time.Second)
	executor, _ := newTestExecutor(policy)
	now := executor.now()
	executor.now = func() time.Time { return now }
	executor.wait = func(context.Context, time.Duration) error {
		now = now.Add(3 * time.Second)
		return nil
	}
	var calls atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return testResponse(http.StatusBadGateway), nil
	})}

	_, failure := executor.Do(context.Background(), client, Metadata{}, Options{Mode: Headers}, testRequestFactory())

	if failure == nil || failure.Attempts != 1 || calls.Load() != 1 {
		t.Fatalf("failure = %+v, calls = %d; want one attempt", failure, calls.Load())
	}
}

func TestRetryWaitIsCancelable(t *testing.T) {
	policy := retryPolicy()
	policy.MaxAttempts = 2
	executor := NewExecutor(policy)
	ctx, cancel := context.WithCancel(context.Background())
	executor.wait = func(waitCtx context.Context, _ time.Duration) error {
		cancel()
		<-waitCtx.Done()
		return waitCtx.Err()
	}

	_, failure := executor.Do(ctx, failingClient(http.StatusBadGateway, nil), Metadata{}, Options{Mode: Headers}, testRequestFactory())

	if failure == nil || failure.Kind != FailureCanceled || !errors.Is(failure.Err, context.Canceled) {
		t.Fatalf("failure = %+v, want canceled", failure)
	}
}

func TestRetryDistinguishesClientCancelAndServiceShutdown(t *testing.T) {
	tests := []struct {
		name  string
		cause error
		want  FailureKind
	}{
		{name: "client", cause: context.Canceled, want: FailureClientCancel},
		{name: "service", cause: ErrServiceShutdown, want: FailureServiceShutdown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancelCause(context.Background())
			cancel(tt.cause)
			executor := NewExecutor(retryPolicy())

			_, failure := executor.Do(ctx, http.DefaultClient, Metadata{}, Options{Mode: Headers}, testRequestFactory())

			if failure == nil || failure.Kind != tt.want || !errors.Is(failure.Err, tt.cause) {
				t.Fatalf("failure = %+v, want kind %q and cause %v", failure, tt.want, tt.cause)
			}
		})
	}
}

func TestRetryFactoryErrorStopsImmediately(t *testing.T) {
	executor, waits := newTestExecutor(retryPolicy())
	want := errors.New("cannot build request")
	var calls atomic.Int32

	_, failure := executor.Do(context.Background(), http.DefaultClient, Metadata{}, Options{Mode: Buffered}, func(context.Context) (*http.Request, error) {
		calls.Add(1)
		return nil, want
	})

	if failure == nil || failure.Kind != FailureLocal || !errors.Is(failure.Err, want) || calls.Load() != 1 || len(*waits) != 0 {
		t.Fatalf("failure = %+v, calls = %d, waits = %v", failure, calls.Load(), *waits)
	}
}

func TestRetryUsesConfiguredHTTPStatusCodes(t *testing.T) {
	tests := []struct {
		name         string
		status       int
		statusCodes  []int
		wantCalls    int32
		wantEligible bool
	}{
		{name: "default excludes 403", status: http.StatusForbidden, statusCodes: config.DefaultConfig().Retry.StatusCodes, wantCalls: 1},
		{name: "configured 403", status: http.StatusForbidden, statusCodes: []int{403}, wantCalls: 2, wantEligible: true},
		{name: "configured 429", status: http.StatusTooManyRequests, statusCodes: []int{429}, wantCalls: 2, wantEligible: true},
		{name: "configured 502", status: http.StatusBadGateway, statusCodes: []int{502}, wantCalls: 2, wantEligible: true},
		{name: "empty list", status: http.StatusBadGateway, statusCodes: []int{}, wantCalls: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy := retryPolicy()
			policy.MaxAttempts = 2
			policy.StatusCodes = tt.statusCodes
			executor, waits := newTestExecutor(policy)
			var calls atomic.Int32
			client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls.Add(1)
				return testResponse(tt.status), nil
			})}

			_, failure := executor.Do(context.Background(), client, Metadata{}, Options{Mode: Headers}, testRequestFactory())

			if failure == nil || failure.StatusCode != tt.status || calls.Load() != tt.wantCalls {
				t.Fatalf("failure = %+v, calls = %d, want %d", failure, calls.Load(), tt.wantCalls)
			}
			if failure.RetryEligible != tt.wantEligible {
				t.Fatalf("RetryEligible = %t, want %t", failure.RetryEligible, tt.wantEligible)
			}
			if got, want := len(*waits), int(tt.wantCalls-1); got != want {
				t.Fatalf("waits = %v, want %d", *waits, want)
			}
		})
	}
}

func TestRetryTransportErrorRecovers(t *testing.T) {
	want := errors.New("connection reset")
	var calls atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			return nil, want
		}
		return testResponse(http.StatusOK), nil
	})}
	executor, waits := newTestExecutor(retryPolicy())

	result, failure := executor.Do(context.Background(), client, Metadata{}, Options{Mode: Headers}, testRequestFactory())

	if failure != nil || result == nil || result.Response.StatusCode != http.StatusOK {
		t.Fatalf("result = %+v, failure = %+v", result, failure)
	}
	result.Response.Body.Close()
	assertDurations(t, *waits, time.Second)
}

func TestRetryRealRedirectStopsAt3xx(t *testing.T) {
	var destinationCalls atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		destinationCalls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer destination.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusFound)
	}))
	defer redirect.Close()
	policy := retryPolicy()
	policy.MaxAttempts = 1
	executor, _ := newTestExecutor(policy)

	_, failure := executor.Do(context.Background(), redirect.Client(), Metadata{}, Options{Mode: Headers}, func(ctx context.Context) (*http.Request, error) {
		return http.NewRequestWithContext(ctx, http.MethodGet, redirect.URL, nil)
	})

	if failure == nil || failure.StatusCode != http.StatusFound || destinationCalls.Load() != 0 {
		t.Fatalf("failure = %+v, destination calls = %d", failure, destinationCalls.Load())
	}
}

func TestBufferedSuccessReadsAndClosesBody(t *testing.T) {
	body := &trackingBody{Reader: strings.NewReader("complete response")}
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		response := testResponse(http.StatusOK)
		response.Body = body
		return response, nil
	})}
	policy := retryPolicy()
	policy.MaxAttempts = 1
	executor, _ := newTestExecutor(policy)

	result, failure := executor.Do(context.Background(), client, Metadata{}, Options{Mode: Buffered}, testRequestFactory())

	if failure != nil || result == nil || string(result.Body) != "complete response" {
		t.Fatalf("result = %+v, failure = %+v", result, failure)
	}
	if body.closeCalls.Load() != 1 {
		t.Fatalf("body close calls = %d, want 1", body.closeCalls.Load())
	}
}

func TestBufferedIgnoresCloseErrorAfterCompleteBodyRead(t *testing.T) {
	var calls atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		response := testResponse(http.StatusOK)
		response.Body = closeErrorBody{
			Reader: strings.NewReader(`{"ok":true}`),
			err:    errors.New("close failed"),
		}
		return response, nil
	})}
	policy := retryPolicy()
	policy.MaxAttempts = 2
	executor, _ := newTestExecutor(policy)

	result, failure := executor.Do(context.Background(), client, Metadata{}, Options{Mode: Buffered}, testRequestFactory())

	if failure != nil || result == nil || string(result.Body) != `{"ok":true}` {
		t.Fatalf("result = %+v, failure = %+v", result, failure)
	}
	if calls.Load() != 1 {
		t.Fatalf("upstream calls = %d, want 1", calls.Load())
	}
}

func TestBufferedBodyReadErrorRetries(t *testing.T) {
	var calls atomic.Int32
	firstBody := &trackingBody{Reader: errorReader{err: errors.New("broken body")}}
	secondBody := &trackingBody{Reader: strings.NewReader("recovered")}
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		response := testResponse(http.StatusOK)
		if calls.Add(1) == 1 {
			response.Body = firstBody
		} else {
			response.Body = secondBody
		}
		return response, nil
	})}
	executor, waits := newTestExecutor(retryPolicy())

	result, failure := executor.Do(context.Background(), client, Metadata{}, Options{Mode: Buffered}, testRequestFactory())

	if failure != nil || result == nil || string(result.Body) != "recovered" {
		t.Fatalf("result = %+v, failure = %+v", result, failure)
	}
	if firstBody.closeCalls.Load() != 1 || secondBody.closeCalls.Load() != 1 {
		t.Fatalf("close calls = %d, %d; want 1, 1", firstBody.closeCalls.Load(), secondBody.closeCalls.Load())
	}
	assertDurations(t, *waits, time.Second)
}

func TestBufferedSuccessBodyLimitIsNonRetryable(t *testing.T) {
	var calls atomic.Int32
	body := &trackingBody{Reader: strings.NewReader("12345")}
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		response := testResponse(http.StatusOK)
		response.Body = body
		return response, nil
	})}
	executor, waits := newTestExecutor(retryPolicy())

	_, failure := executor.Do(context.Background(), client, Metadata{}, Options{Mode: Buffered, SuccessBodyLimit: 4}, testRequestFactory())

	if failure == nil || failure.Kind != FailureProtocol || failure.StatusCode != http.StatusBadGateway {
		t.Fatalf("failure = %+v, want non-retryable 502 protocol failure", failure)
	}
	if calls.Load() != 1 || body.closeCalls.Load() != 1 || len(*waits) != 0 {
		t.Fatalf("calls = %d, closes = %d, waits = %v", calls.Load(), body.closeCalls.Load(), *waits)
	}
}

func TestBufferedZeroBodyLimitIsUnlimited(t *testing.T) {
	payload := strings.Repeat("x", 2<<20)
	executor, _ := newTestExecutor(retryPolicy())
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		response := testResponse(http.StatusOK)
		response.Body = &trackingBody{Reader: strings.NewReader(payload)}
		return response, nil
	})}

	result, failure := executor.Do(context.Background(), client, Metadata{}, Options{Mode: Buffered, SuccessBodyLimit: 0}, testRequestFactory())

	if failure != nil || result == nil || string(result.Body) != payload {
		t.Fatalf("body length = %d, failure = %+v", len(result.Body), failure)
	}
}

func TestHeadersSuccessStopsAttemptTimeoutAndKeepsParentCancellation(t *testing.T) {
	var upstreamContext context.Context
	body := &trackingBody{Reader: strings.NewReader("stream")}
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		upstreamContext = request.Context()
		response := testResponse(http.StatusOK)
		response.Body = body
		return response, nil
	})}
	policy := retryPolicy()
	policy.MaxAttempts = 1
	executor := NewExecutor(policy)
	parent, cancelParent := context.WithCancel(context.Background())
	defer cancelParent()

	result, failure := executor.Do(parent, client, Metadata{}, Options{Mode: Headers, PerAttemptTimeout: 20 * time.Millisecond}, testRequestFactory())

	if failure != nil || result == nil {
		t.Fatalf("result = %+v, failure = %+v", result, failure)
	}
	select {
	case <-upstreamContext.Done():
		t.Fatalf("successful headers context canceled by attempt timeout: %v", context.Cause(upstreamContext))
	case <-time.After(50 * time.Millisecond):
	}
	cancelParent()
	select {
	case <-upstreamContext.Done():
	case <-time.After(time.Second):
		t.Fatal("successful headers context did not retain parent cancellation")
	}
	if err := result.Response.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if body.closeCalls.Load() != 1 {
		t.Fatalf("body close calls = %d, want 1", body.closeCalls.Load())
	}
}

func TestHeadersBodyCloseReleasesAttemptContext(t *testing.T) {
	var upstreamContext context.Context
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		upstreamContext = request.Context()
		return testResponse(http.StatusOK), nil
	})}
	policy := retryPolicy()
	policy.MaxAttempts = 1
	executor := NewExecutor(policy)

	result, failure := executor.Do(context.Background(), client, Metadata{}, Options{Mode: Headers, PerAttemptTimeout: time.Minute}, testRequestFactory())
	if failure != nil {
		t.Fatal(failure)
	}
	if err := result.Response.Body.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-upstreamContext.Done():
	case <-time.After(time.Second):
		t.Fatal("closing successful headers body did not release attempt context")
	}
}

func TestHeadersBodyCloseIsIdempotent(t *testing.T) {
	body := &trackingBody{Reader: strings.NewReader("stream")}
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		response := testResponse(http.StatusOK)
		response.Body = body
		return response, nil
	})}
	policy := retryPolicy()
	policy.MaxAttempts = 1
	executor := NewExecutor(policy)

	result, failure := executor.Do(context.Background(), client, Metadata{}, Options{Mode: Headers}, testRequestFactory())
	if failure != nil {
		t.Fatal(failure)
	}
	_ = result.Response.Body.Close()
	_ = result.Response.Body.Close()

	if body.closeCalls.Load() != 1 {
		t.Fatalf("underlying body close calls = %d, want 1", body.closeCalls.Load())
	}
}

func TestHeadersResponseAfterAttemptTimeoutIsFailure(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		<-request.Context().Done()
		return testResponse(http.StatusOK), nil
	})}
	policy := retryPolicy()
	policy.MaxAttempts = 1
	executor := NewExecutor(policy)

	result, failure := executor.Do(context.Background(), client, Metadata{}, Options{Mode: Headers, PerAttemptTimeout: 10 * time.Millisecond}, testRequestFactory())

	if result != nil || failure == nil || failure.Kind != FailureTimeout {
		t.Fatalf("result = %+v, failure = %+v; want timeout", result, failure)
	}
}

func TestBufferedBodyTimeoutReturns504(t *testing.T) {
	var body *contextBlockingBody
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		body = &contextBlockingBody{ctx: request.Context()}
		response := testResponse(http.StatusOK)
		response.Body = body
		return response, nil
	})}
	policy := retryPolicy()
	policy.MaxAttempts = 1
	executor := NewExecutor(policy)

	_, failure := executor.Do(context.Background(), client, Metadata{}, Options{Mode: Buffered, PerAttemptTimeout: 10 * time.Millisecond}, testRequestFactory())

	if failure == nil || failure.Kind != FailureTimeout || failure.StatusCode != http.StatusGatewayTimeout {
		t.Fatalf("failure = %+v, want 504 timeout", failure)
	}
	if body.closeCalls.Load() != 1 {
		t.Fatalf("body close calls = %d, want 1", body.closeCalls.Load())
	}
}

func TestBufferedResponseCompletingAfterTimeoutReturns504(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		response := testResponse(http.StatusOK)
		response.Body = &contextSuccessBody{ctx: request.Context()}
		return response, nil
	})}
	policy := retryPolicy()
	policy.MaxAttempts = 1
	executor := NewExecutor(policy)

	result, failure := executor.Do(context.Background(), client, Metadata{}, Options{Mode: Buffered, PerAttemptTimeout: 10 * time.Millisecond}, testRequestFactory())

	if result != nil || failure == nil || failure.Kind != FailureTimeout || failure.StatusCode != http.StatusGatewayTimeout {
		t.Fatalf("result = %+v, failure = %+v; want 504 timeout", result, failure)
	}
}

func TestAttemptTimeoutUsesRemainingRetryBudget(t *testing.T) {
	policy := retryPolicy()
	policy.MaxAttempts = 1
	policy.MaxElapsed = config.Duration(20 * time.Millisecond)
	executor := NewExecutor(policy)
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		<-request.Context().Done()
		return nil, request.Context().Err()
	})}
	started := time.Now()

	_, failure := executor.Do(context.Background(), client, Metadata{}, Options{Mode: Headers, PerAttemptTimeout: time.Minute}, testRequestFactory())

	if failure == nil || failure.Kind != FailureTimeout || failure.StatusCode != http.StatusGatewayTimeout {
		t.Fatalf("failure = %+v, want remaining-budget timeout", failure)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("attempt took %s; remaining budget was not applied", elapsed)
	}
}

func TestFailedResponseBodyIsLimitedAndClosed(t *testing.T) {
	reader := &countingReader{data: []byte(strings.Repeat("x", 10_000))}
	body := &trackingBody{Reader: reader}
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		response := testResponse(http.StatusBadGateway)
		response.Body = body
		return response, nil
	})}
	policy := retryPolicy()
	policy.MaxAttempts = 1
	executor, _ := newTestExecutor(policy)

	_, failure := executor.Do(context.Background(), client, Metadata{}, Options{Mode: Headers}, testRequestFactory())

	if failure == nil || failure.StatusCode != http.StatusBadGateway {
		t.Fatalf("failure = %+v", failure)
	}
	if reader.bytesRead != 4097 || body.closeCalls.Load() != 1 {
		t.Fatalf("bytes read = %d, closes = %d; want 4097 and 1", reader.bytesRead, body.closeCalls.Load())
	}
}

func TestCancellationWhileReadingFailedBodyWins(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	var body *trackingBody
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		body = &trackingBody{Reader: readerFunc(func([]byte) (int, error) {
			cancel()
			<-request.Context().Done()
			return 0, request.Context().Err()
		})}
		response := testResponse(http.StatusBadGateway)
		response.Body = body
		return response, nil
	})}
	policy := retryPolicy()
	policy.MaxAttempts = 1
	executor := NewExecutor(policy)

	_, failure := executor.Do(parent, client, Metadata{}, Options{Mode: Headers, PerAttemptTimeout: time.Minute}, testRequestFactory())

	if failure == nil || failure.Kind != FailureCanceled || !errors.Is(failure.Err, context.Canceled) {
		t.Fatalf("failure = %+v, want client cancellation", failure)
	}
	if body.closeCalls.Load() != 1 {
		t.Fatalf("body close calls = %d, want 1", body.closeCalls.Load())
	}
}

func TestFailureStoresOnlySanitizedLastHTTPBody(t *testing.T) {
	const secret = "provider-secret"
	body := `{"api_key":"provider-secret","message":"capacity exhausted"}`
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		response := testResponse(http.StatusBadGateway)
		response.Body = io.NopCloser(strings.NewReader(body))
		return response, nil
	})}
	policy := retryPolicy()
	policy.MaxAttempts = 1
	executor, _ := newTestExecutor(policy)

	_, failure := executor.Do(context.Background(), client, Metadata{}, Options{
		Mode:      Headers,
		Sanitizer: NewSanitizer(secret),
	}, testRequestFactory())

	if failure == nil || !strings.Contains(failure.Summary, "capacity exhausted") {
		t.Fatalf("failure = %+v", failure)
	}
	if strings.Contains(failure.Summary, secret) || strings.Contains(failure.Err.Error(), secret) {
		t.Fatalf("failure retained secret: %+v", failure)
	}
}

func TestFailureRedactsConfiguredSecretPrefixAtReadLimit(t *testing.T) {
	const secret = "LeakPrefix-Provider-Secret"
	body := strings.Repeat("x", 4090) + secret + "tail"
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		response := testResponse(http.StatusBadGateway)
		response.Body = io.NopCloser(strings.NewReader(body))
		return response, nil
	})}
	policy := retryPolicy()
	policy.MaxAttempts = 1
	executor, _ := newTestExecutor(policy)

	_, failure := executor.Do(context.Background(), client, Metadata{}, Options{
		Mode:      Headers,
		Sanitizer: NewSanitizer(secret),
	}, testRequestFactory())

	if failure == nil {
		t.Fatal("expected failure")
	}
	if leakedPrefix := secret[:6]; strings.Contains(strings.ToLower(failure.Summary), strings.ToLower(leakedPrefix)) {
		t.Fatalf("failure leaked configured secret prefix %q at read limit: %q", leakedPrefix, failure.Summary[len(failure.Summary)-64:])
	}
}

func TestFailureRedactsUnicodeSecretPrefixAtReadLimit(t *testing.T) {
	const secret = "Key-密钥"
	body := strings.Repeat("x", 4092) + secret + "tail"
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		response := testResponse(http.StatusBadGateway)
		response.Body = io.NopCloser(strings.NewReader(body))
		return response, nil
	})}
	policy := retryPolicy()
	policy.MaxAttempts = 1
	executor, _ := newTestExecutor(policy)

	_, failure := executor.Do(context.Background(), client, Metadata{}, Options{
		Mode:      Headers,
		Sanitizer: NewSanitizer(secret),
	}, testRequestFactory())

	if failure == nil {
		t.Fatal("expected failure")
	}
	if strings.Contains(strings.ToLower(failure.Summary), "key-") {
		t.Fatalf("failure leaked configured Unicode secret prefix at read limit: %q", failure.Summary[len(failure.Summary)-64:])
	}
}

func TestFailureSanitizesTransportError(t *testing.T) {
	const secret = "transport-secret"
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("dial failed with Bearer " + secret)
	})}
	policy := retryPolicy()
	policy.MaxAttempts = 1
	executor, _ := newTestExecutor(policy)

	_, failure := executor.Do(context.Background(), client, Metadata{}, Options{
		Mode:      Headers,
		Sanitizer: NewSanitizer(secret),
	}, testRequestFactory())

	if failure == nil || failure.Kind != FailureTransport || failure.Summary == "" {
		t.Fatalf("failure = %+v", failure)
	}
	if strings.Contains(failure.Summary, secret) || strings.Contains(failure.Err.Error(), secret) {
		t.Fatalf("transport failure retained secret: %+v", failure)
	}
}

func TestNativeTransportTimeoutReturns504(t *testing.T) {
	policy := retryPolicy()
	policy.MaxAttempts = 1
	executor, _ := newTestExecutor(policy)
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, context.DeadlineExceeded
	})}

	_, failure := executor.Do(context.Background(), client, Metadata{}, Options{Mode: Headers}, testRequestFactory())

	if failure == nil || failure.Kind != FailureTimeout || failure.StatusCode != http.StatusGatewayTimeout {
		t.Fatalf("failure = %+v, want native transport timeout mapped to 504", failure)
	}
}

func TestNativeBufferedBodyTimeoutReturns504(t *testing.T) {
	policy := retryPolicy()
	policy.MaxAttempts = 1
	executor, _ := newTestExecutor(policy)
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		response := testResponse(http.StatusOK)
		response.Body = io.NopCloser(errorReader{err: timeoutError{}})
		return response, nil
	})}

	_, failure := executor.Do(context.Background(), client, Metadata{}, Options{Mode: Buffered}, testRequestFactory())

	if failure == nil || failure.Kind != FailureTimeout || failure.StatusCode != http.StatusGatewayTimeout {
		t.Fatalf("failure = %+v, want native body timeout mapped to 504", failure)
	}
}

func TestFailedHTTPStatusSurvivesDiagnosticBodyTimeout(t *testing.T) {
	policy := retryPolicy()
	policy.MaxAttempts = 1
	executor := NewExecutor(policy)
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		response := testResponse(http.StatusTooManyRequests)
		response.Body = io.NopCloser(readerFunc(func([]byte) (int, error) {
			<-request.Context().Done()
			return 0, request.Context().Err()
		}))
		return response, nil
	})}

	_, failure := executor.Do(context.Background(), client, Metadata{}, Options{
		Mode:              Headers,
		PerAttemptTimeout: 10 * time.Millisecond,
	}, testRequestFactory())

	if failure == nil || failure.Kind != FailureHTTP || failure.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("failure = %+v, want known HTTP 429 preserved", failure)
	}
}

func TestResponseAndErrorBodyIsLimitedAndClosed(t *testing.T) {
	reader := &countingReader{data: []byte(strings.Repeat("x", 10_000))}
	body := &trackingBody{Reader: reader}
	policy := retryPolicy()
	policy.MaxAttempts = 1
	executor, _ := newTestExecutor(policy)
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		response := testResponse(http.StatusBadGateway)
		response.Body = body
		return response, errors.New("transport failed after response")
	})}

	_, failure := executor.Do(context.Background(), client, Metadata{}, Options{Mode: Headers}, testRequestFactory())

	if failure == nil {
		t.Fatal("expected failure")
	}
	if reader.bytesRead != 4097 || body.closeCalls.Load() != 1 {
		t.Fatalf("bytes read = %d, closes = %d; want 4097 and 1", reader.bytesRead, body.closeCalls.Load())
	}
}

func TestRetryShallowCopiesClientAndDisablesRedirects(t *testing.T) {
	redirectCalls := 0
	originalRedirect := func(*http.Request, []*http.Request) error {
		redirectCalls++
		return nil
	}
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		return testResponse(http.StatusFound), nil
	})
	client := &http.Client{Transport: transport, CheckRedirect: originalRedirect}
	policy := retryPolicy()
	policy.MaxAttempts = 1
	executor, _ := newTestExecutor(policy)

	_, failure := executor.Do(context.Background(), client, Metadata{}, Options{Mode: Headers}, testRequestFactory())

	sameTransport := reflect.ValueOf(client.Transport).Pointer() == reflect.ValueOf(transport).Pointer()
	if failure == nil || failure.StatusCode != http.StatusFound || client.CheckRedirect == nil || redirectCalls != 0 || !sameTransport {
		t.Fatalf("failure = %+v; original client changed or redirect followed", failure)
	}
}

func TestRetryLogsFailuresAndRecoveryWithoutSecrets(t *testing.T) {
	const secret = "log-secret"
	policy := retryPolicy()
	policy.MaxAttempts = 3
	executor, _ := newTestExecutor(policy)
	var output bytes.Buffer
	executor.logger = slog.New(slog.NewJSONHandler(&output, nil))
	var calls atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		response := testResponse(http.StatusBadGateway)
		if calls.Add(1) == 3 {
			response.StatusCode = http.StatusOK
			response.Body = io.NopCloser(strings.NewReader("recovered"))
			return response, nil
		}
		response.Body = io.NopCloser(strings.NewReader(`{"authorization":"Bearer log-secret","message":"temporary"}`))
		return response, nil
	})}
	metadata := Metadata{RequestID: "req-123", Provider: "c78", Model: "gpt-5.6-sol", Endpoint: "responses"}

	result, failure := executor.Do(context.Background(), client, metadata, Options{
		Mode:      Buffered,
		Sanitizer: NewSanitizer(secret),
	}, testRequestFactory())

	if failure != nil || result == nil || result.Attempts != 3 {
		t.Fatalf("result = %+v, failure = %+v", result, failure)
	}
	records := decodeJSONLogRecords(t, output.Bytes())
	if len(records) != 3 {
		t.Fatalf("log records = %#v", records)
	}
	for index := 0; index < 2; index++ {
		record := records[index]
		if record["msg"] != "upstream attempt failed" || record["request_id"] != "req-123" || record["provider"] != "c78" || record["model"] != "gpt-5.6-sol" || record["endpoint"] != "responses" {
			t.Fatalf("failure record %d = %#v", index, record)
		}
		if record["failure_kind"] != "http" || record["status"] != float64(http.StatusBadGateway) || record["attempt"] != float64(index+1) || record["max_attempts"] != float64(3) {
			t.Fatalf("failure fields %d = %#v", index, record)
		}
		if _, ok := record["delay_ms"]; !ok {
			t.Fatalf("failure record lacks delay_ms: %#v", record)
		}
		if _, ok := record["elapsed_ms"]; !ok {
			t.Fatalf("failure record lacks elapsed_ms: %#v", record)
		}
	}
	if records[2]["msg"] != "upstream recovered" || records[2]["attempts"] != float64(3) {
		t.Fatalf("recovery record = %#v", records[2])
	}
	if strings.Contains(output.String(), secret) || strings.Contains(output.String(), "Bearer") {
		t.Fatalf("logs contain credential: %s", output.String())
	}
}

func TestRetryLogsExhaustionAndCancellationKinds(t *testing.T) {
	t.Run("exhausted", func(t *testing.T) {
		policy := retryPolicy()
		policy.MaxAttempts = 1
		executor, _ := newTestExecutor(policy)
		var output bytes.Buffer
		executor.logger = slog.New(slog.NewJSONHandler(&output, nil))

		_, failure := executor.Do(context.Background(), failingClient(http.StatusBadGateway, nil), Metadata{RequestID: "req-exhausted", Provider: "mars"}, Options{Mode: Headers}, testRequestFactory())

		if failure == nil || !failure.RetryEligible {
			t.Fatalf("failure = %+v, want retry-eligible failure", failure)
		}
		records := decodeJSONLogRecords(t, output.Bytes())
		if len(records) != 2 || records[1]["msg"] != "upstream retry exhausted" || records[1]["failure_kind"] != "http" || records[1]["status"] != float64(http.StatusBadGateway) {
			t.Fatalf("records = %#v", records)
		}
	})

	t.Run("skipped", func(t *testing.T) {
		policy := retryPolicy()
		policy.MaxAttempts = 3
		executor, _ := newTestExecutor(policy)
		var output bytes.Buffer
		executor.logger = slog.New(slog.NewJSONHandler(&output, nil))

		_, failure := executor.Do(context.Background(), failingClient(http.StatusForbidden, nil), Metadata{RequestID: "req-skipped", Provider: "mars"}, Options{Mode: Headers}, testRequestFactory())

		if failure == nil || failure.RetryEligible || failure.Attempts != 1 {
			t.Fatalf("failure = %+v, want ineligible single-attempt failure", failure)
		}
		records := decodeJSONLogRecords(t, output.Bytes())
		if len(records) != 2 || records[1]["msg"] != "upstream retry skipped" || records[1]["failure_kind"] != "http" || records[1]["status"] != float64(http.StatusForbidden) {
			t.Fatalf("records = %#v", records)
		}
	})

	for _, test := range []struct {
		name  string
		cause error
		kind  FailureKind
	}{
		{name: "client", cause: context.Canceled, kind: FailureClientCancel},
		{name: "service", cause: ErrServiceShutdown, kind: FailureServiceShutdown},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancelCause(context.Background())
			cancel(test.cause)
			executor := NewExecutor(retryPolicy())
			var output bytes.Buffer
			executor.logger = slog.New(slog.NewJSONHandler(&output, nil))

			_, failure := executor.Do(ctx, http.DefaultClient, Metadata{RequestID: "req-cancel", Provider: "ds"}, Options{Mode: Headers}, testRequestFactory())

			if failure == nil || failure.Kind != test.kind {
				t.Fatalf("failure = %+v", failure)
			}
			records := decodeJSONLogRecords(t, output.Bytes())
			if len(records) != 1 || records[0]["msg"] != "upstream retry canceled" || records[0]["failure_kind"] != string(test.kind) {
				t.Fatalf("records = %#v", records)
			}
		})
	}
}

func TestRetryLogsCancellationDuringAttemptWithoutRetryFailure(t *testing.T) {
	for _, test := range []struct {
		name  string
		cause error
		kind  FailureKind
	}{
		{name: "client", cause: context.Canceled, kind: FailureClientCancel},
		{name: "service", cause: ErrServiceShutdown, kind: FailureServiceShutdown},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancelCause(context.Background())
			executor, _ := newTestExecutor(retryPolicy())
			var output bytes.Buffer
			executor.logger = slog.New(slog.NewJSONHandler(&output, nil))
			client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				cancel(test.cause)
				<-request.Context().Done()
				return nil, request.Context().Err()
			})}

			_, failure := executor.Do(ctx, client, Metadata{RequestID: "req-cancel", Provider: "ds"}, Options{Mode: Headers}, testRequestFactory())

			if failure == nil || failure.Kind != test.kind {
				t.Fatalf("failure = %+v", failure)
			}
			records := decodeJSONLogRecords(t, output.Bytes())
			if len(records) != 1 || records[0]["msg"] != "upstream retry canceled" || records[0]["failure_kind"] != string(test.kind) {
				t.Fatalf("records = %#v", records)
			}
		})
	}
}

func TestRetryUpdatesRequestMeta(t *testing.T) {
	policy := retryPolicy()
	policy.MaxAttempts = 2
	executor, _ := newTestExecutor(policy)
	requestMeta := &onellmLog.RequestMeta{}
	ctx := onellmLog.WithRequestMeta(context.Background(), requestMeta)
	var calls atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			return testResponse(http.StatusBadGateway), nil
		}
		return testResponse(http.StatusOK), nil
	})}

	result, failure := executor.Do(ctx, client, Metadata{}, Options{Mode: Buffered}, testRequestFactory())

	if failure != nil || result == nil {
		t.Fatalf("result = %+v, failure = %+v", result, failure)
	}
	if requestMeta.UpstreamAttempts != 2 || requestMeta.RetryElapsedMs != 1000 || requestMeta.LastUpstreamStatus != http.StatusBadGateway || requestMeta.LastFailureKind != string(FailureHTTP) {
		t.Fatalf("request meta = %+v", requestMeta)
	}
}

func decodeJSONLogRecords(t *testing.T, data []byte) []map[string]any {
	t.Helper()
	lines := bytes.Split(bytes.TrimSpace(data), []byte("\n"))
	if len(lines) == 1 && len(lines[0]) == 0 {
		return nil
	}
	records := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		var record map[string]any
		if err := json.Unmarshal(line, &record); err != nil {
			t.Fatalf("decode log %q: %v", line, err)
		}
		records = append(records, record)
	}
	return records
}

func retryPolicy() config.RetryConfig {
	policy := config.DefaultConfig().Retry
	policy.MaxAttempts = 4
	return policy
}

func newTestExecutor(policy config.RetryConfig) (*Executor, *[]time.Duration) {
	executor := NewExecutor(policy)
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	waits := []time.Duration{}
	executor.now = func() time.Time { return now }
	executor.wait = func(_ context.Context, delay time.Duration) error {
		waits = append(waits, delay)
		now = now.Add(delay)
		return nil
	}
	executor.jitter = func() float64 { return 0.5 }
	return executor, &waits
}

func testRequestFactory() RequestFactory {
	return func(ctx context.Context) (*http.Request, error) {
		return http.NewRequestWithContext(ctx, http.MethodPost, "https://example.test/v1/messages", strings.NewReader("body"))
	}
}

func failingClient(status int, header http.Header) *http.Client {
	return &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		response := testResponse(status)
		response.Header = header.Clone()
		return response, nil
	})}
}

func testResponse(status int) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader("body")),
	}
}

func assertDurations(t *testing.T, got []time.Duration, want ...time.Duration) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("durations = %v, want %v", got, want)
	}
	for index := range got {
		if got[index] != want[index] {
			t.Fatalf("durations = %v, want %v", got, want)
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type trackingBody struct {
	io.Reader
	closeCalls atomic.Int32
}

type closeErrorBody struct {
	io.Reader
	err error
}

func (b closeErrorBody) Close() error {
	return b.err
}

func (b *trackingBody) Close() error {
	b.closeCalls.Add(1)
	return nil
}

type errorReader struct {
	err error
}

func (r errorReader) Read([]byte) (int, error) {
	return 0, r.err
}

type timeoutError struct{}

func (timeoutError) Error() string   { return "native timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

type contextBlockingBody struct {
	ctx        context.Context
	closeCalls atomic.Int32
}

func (b *contextBlockingBody) Read([]byte) (int, error) {
	<-b.ctx.Done()
	return 0, b.ctx.Err()
}

func (b *contextBlockingBody) Close() error {
	b.closeCalls.Add(1)
	return nil
}

type contextSuccessBody struct {
	ctx  context.Context
	read bool
}

func (b *contextSuccessBody) Read(buffer []byte) (int, error) {
	if b.read {
		return 0, io.EOF
	}
	<-b.ctx.Done()
	b.read = true
	return copy(buffer, "late success"), io.EOF
}

func (b *contextSuccessBody) Close() error {
	return nil
}

type countingReader struct {
	data      []byte
	bytesRead int
}

type readerFunc func([]byte) (int, error)

func (f readerFunc) Read(buffer []byte) (int, error) {
	return f(buffer)
}

func (r *countingReader) Read(buffer []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	if len(buffer) > len(r.data) {
		buffer = buffer[:len(r.data)]
	}
	n := copy(buffer, r.data)
	r.data = r.data[n:]
	r.bytesRead += n
	return n, nil
}
