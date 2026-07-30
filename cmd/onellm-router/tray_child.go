package main

import (
	"bufio"
	"io"
	"strings"
)

func watchTrayControl(input io.Reader, stop func()) {
	scanner := bufio.NewScanner(input)
	for scanner.Scan() {
		if strings.EqualFold(strings.TrimSpace(scanner.Text()), "shutdown") {
			stop()
			return
		}
	}
}

func shouldStartNativeTray(trayChild bool) bool {
	return !trayChild
}

func shouldDetachFromTerminal(daemon, trayChild bool) bool {
	return daemon && !trayChild
}
