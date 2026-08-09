//go:build !windows

package main

import (
	"strings"
	"testing"
)

func TestUnixLifecycleCommandsAreUnsupported(t *testing.T) {
	for _, command := range []string{"install", "uninstall"} {
		root := newRootCmd()
		root.SetArgs([]string{command})
		err := root.Execute()
		if err == nil || !strings.Contains(err.Error(), "unsupported on this platform") {
			t.Fatalf("%s error = %v", command, err)
		}
	}
}

func TestUnixDaemonDetachIsUnsupported(t *testing.T) {
	err := validatePlatformLifecycle(true, false)
	if err == nil || err.Error() != "--daemon is unsupported on this platform" {
		t.Fatalf("validation error = %v", err)
	}
	if err := validatePlatformLifecycle(true, true); err != nil {
		t.Fatalf("tray child validation error = %v", err)
	}
}
