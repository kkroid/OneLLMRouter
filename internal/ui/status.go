package ui

import "sync/atomic"

// TrayStatus represents the current tray icon state.
type TrayStatus int32

const (
	StatusHealthy  TrayStatus = 0 // green — service OK, no recent errors
	StatusDegraded TrayStatus = 1 // yellow — recent upstream errors
	StatusError    TrayStatus = 2 // red — health check failed
)

var (
	currentStatus  int32 // atomic TrayStatus
	upstreamErrors int64 // atomic counter, reset on each health poll
)

// RecordUpstreamError increments the 502 error counter.
// Called by the proxy handler on StatusBadGateway responses.
func RecordUpstreamError() {
	atomic.AddInt64(&upstreamErrors, 1)
}

// GetAndResetErrors atomically reads and resets the error counter.
// Returns the count since the last poll.
func GetAndResetErrors() int64 {
	return atomic.SwapInt64(&upstreamErrors, 0)
}

// SetTrayStatus atomically sets the current tray status.
func SetTrayStatus(s TrayStatus) {
	atomic.StoreInt32(&currentStatus, int32(s))
}

// GetTrayStatus atomically reads the current tray status.
func GetTrayStatus() TrayStatus {
	return TrayStatus(atomic.LoadInt32(&currentStatus))
}
