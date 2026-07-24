package ui

import (
	"encoding/binary"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
)

func TestStatusConstants(t *testing.T) {
	if StatusHealthy != 0 {
		t.Fatalf("StatusHealthy should be 0, got %d", StatusHealthy)
	}
	if StatusDegraded != 1 {
		t.Fatalf("StatusDegraded should be 1, got %d", StatusDegraded)
	}
	if StatusError != 2 {
		t.Fatalf("StatusError should be 2, got %d", StatusError)
	}
}

func TestRecordAndGetErrors(t *testing.T) {
	// Reset to known state
	GetAndResetErrors()

	RecordUpstreamError()
	RecordUpstreamError()

	count := GetAndResetErrors()
	if count != 2 {
		t.Fatalf("expected 2 errors, got %d", count)
	}

	// Should be reset after GetAndResetErrors
	count = GetAndResetErrors()
	if count != 0 {
		t.Fatalf("expected 0 after reset, got %d", count)
	}
}

func TestSetAndGetTrayStatus(t *testing.T) {
	SetTrayStatus(StatusHealthy)
	if s := GetTrayStatus(); s != StatusHealthy {
		t.Fatalf("expected Healthy, got %d", s)
	}

	SetTrayStatus(StatusDegraded)
	if s := GetTrayStatus(); s != StatusDegraded {
		t.Fatalf("expected Degraded, got %d", s)
	}

	SetTrayStatus(StatusError)
	if s := GetTrayStatus(); s != StatusError {
		t.Fatalf("expected Error, got %d", s)
	}
}

func TestEmbeddedIconsExist(t *testing.T) {
	if len(greenIconBytes) == 0 {
		t.Fatal("greenIconBytes is empty — go:embed failed")
	}
	if len(yellowIconBytes) == 0 {
		t.Fatal("yellowIconBytes is empty — go:embed failed")
	}
	if len(redIconBytes) == 0 {
		t.Fatal("redIconBytes is empty — go:embed failed")
	}
}

func TestEmbeddedIconsUseColoredRoundedBadgeWithWhiteHexagon(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		bg   [4]byte
	}{
		{name: "green", data: greenIconBytes, bg: [4]byte{0x2E, 0x8B, 0x57, 0xFF}},
		{name: "yellow", data: yellowIconBytes, bg: [4]byte{0xE0, 0xA0, 0x00, 0xFF}},
		{name: "red", data: redIconBytes, bg: [4]byte{0xC0, 0x39, 0x2B, 0xFF}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := iconPixel(t, test.data, 8, 8); got != [4]byte{0xFF, 0xFF, 0xFF, 0xFF} {
				t.Fatalf("center pixel = %#v, want opaque white", got)
			}
			if got := iconPixel(t, test.data, 1, 8); !colorClose(got, test.bg, 5) {
				t.Fatalf("badge pixel = %#v, want %#v", got, test.bg)
			}
			if got := iconPixel(t, test.data, 0, 0); got[3] != 0 {
				t.Fatalf("corner alpha = %d, want transparent", got[3])
			}
		})
	}
}

func colorClose(got, want [4]byte, tolerance int) bool {
	for i := range got {
		delta := int(got[i]) - int(want[i])
		if delta < -tolerance || delta > tolerance {
			return false
		}
	}
	return true
}

func iconPixel(t *testing.T, data []byte, x, y int) [4]byte {
	t.Helper()
	if len(data) < 40 {
		t.Fatalf("icon resource too short: %d bytes", len(data))
	}
	width := int(int32(binary.LittleEndian.Uint32(data[4:8])))
	doubledHeight := int(int32(binary.LittleEndian.Uint32(data[8:12])))
	height := doubledHeight / 2
	if width != 16 || height != 16 {
		t.Fatalf("icon dimensions = %dx%d, want 16x16", width, height)
	}
	offset := 40 + ((height-1-y)*width+x)*4
	if offset+4 > len(data) {
		t.Fatalf("pixel (%d,%d) exceeds icon resource", x, y)
	}
	b, g, r, a := data[offset], data[offset+1], data[offset+2], data[offset+3]
	return [4]byte{r, g, b, a}
}

func TestBlueIconBytes(t *testing.T) {
	b := blueIconBytes()
	if len(b) < 40 {
		t.Fatalf("blue icon resource too short: %d bytes", len(b))
	}
}

func TestPollHealthClassifiesBadStatusAndMalformedJSONAsDown(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
	}{
		{name: "bad status", status: http.StatusBadGateway, body: `{"status":"error"}`},
		{name: "malformed json", status: http.StatusOK, body: `{"status":`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			GetAndResetErrors()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(test.status)
				w.Write([]byte(test.body))
			}))
			defer server.Close()
			tray := trayForTestServer(t, server)

			tray.pollHealthWithClient(server.Client())

			if tray.health.healthy || tray.health.statusText != "down" {
				t.Fatalf("health = %+v", tray.health)
			}
			if status := GetTrayStatus(); status != StatusError {
				t.Fatalf("status = %v, want error", status)
			}
		})
	}
}

func TestPollHealthClassifiesHealthyAndDegraded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok","models":7,"copilot_token":true}`))
	}))
	defer server.Close()
	tray := trayForTestServer(t, server)

	GetAndResetErrors()
	tray.pollHealthWithClient(server.Client())
	if !tray.health.healthy || tray.health.statusText != "healthy" || tray.health.modelCount != 7 || !tray.health.copilotToken {
		t.Fatalf("healthy snapshot = %+v", tray.health)
	}
	if status := GetTrayStatus(); status != StatusHealthy {
		t.Fatalf("status = %v, want healthy", status)
	}

	RecordUpstreamError()
	tray.pollHealthWithClient(server.Client())
	if !tray.health.healthy || tray.health.statusText != "degraded" || tray.health.errorCount != 1 {
		t.Fatalf("degraded snapshot = %+v", tray.health)
	}
	if status := GetTrayStatus(); status != StatusDegraded {
		t.Fatalf("status = %v, want degraded", status)
	}
}

func trayForTestServer(t *testing.T, server *httptest.Server) *Tray {
	t.Helper()
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	_, portText, err := net.SplitHostPort(parsed.Host)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	return NewTray(port, "test", nil, nil)
}
