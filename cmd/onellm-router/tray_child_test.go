package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestWatchTrayControlStopsOnlyForShutdown(t *testing.T) {
	called := 0
	watchTrayControl(strings.NewReader("ignored\n  ShUtDoWn  \nshutdown\n"), func() { called++ })
	if called != 1 {
		t.Fatalf("stop calls = %d, want 1", called)
	}
}

func TestWatchTrayControlStopsOnEOF(t *testing.T) {
	called := 0
	watchTrayControl(strings.NewReader("ignored\n"), func() { called++ })
	if called != 1 {
		t.Fatalf("stop calls = %d, want 1", called)
	}
}

func TestTrayChildLifecycleDecisions(t *testing.T) {
	if !shouldWatchTrayControl(true) {
		t.Fatal("tray child did not watch Qt control input")
	}
	if shouldDetachFromTerminal(true, true) {
		t.Fatal("tray child detached from terminal")
	}
	if shouldWatchTrayControl(false) {
		t.Fatal("portable mode watched tray control input")
	}
	if !shouldDetachFromTerminal(true, false) {
		t.Fatal("daemon mode did not detach")
	}
}

func TestRootAndExplicitServeUseSameHandler(t *testing.T) {
	root := newRootCmd()
	defaultServe, _, err := root.Find([]string{"--tray-child", "--config", "C:/tmp/config.yaml"})
	if err != nil {
		t.Fatal(err)
	}
	explicitServe, _, err := root.Find([]string{"serve", "--tray-child", "--config", "C:/tmp/config.yaml"})
	if err != nil {
		t.Fatal(err)
	}
	if defaultServe != root || explicitServe.Name() != "serve" {
		t.Fatalf("commands = %q and %q", defaultServe.Name(), explicitServe.Name())
	}
	if reflect.ValueOf(defaultServe.RunE).Pointer() != reflect.ValueOf(explicitServe.RunE).Pointer() {
		t.Fatal("root and explicit serve use different handlers")
	}
	for _, name := range []string{"tray-child", "config"} {
		if root.PersistentFlags().Lookup(name) == nil {
			t.Fatalf("missing persistent --%s flag", name)
		}
	}
}
