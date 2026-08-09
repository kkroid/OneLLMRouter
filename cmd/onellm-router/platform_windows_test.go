//go:build windows

package main

import "testing"

func TestPlatformExecutableNameWindows(t *testing.T) {
	if got := platformExecutableName("onellm-router"); got != "onellm-router.exe" {
		t.Fatalf("executable name = %q, want %q", got, "onellm-router.exe")
	}
}
