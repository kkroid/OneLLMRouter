//go:build !windows

package main

import "github.com/spf13/cobra"

func validatePlatformLifecycle(daemon, trayChild bool) error {
	if daemon && !trayChild {
		return unsupportedPlatformOperation("--daemon")
	}
	return nil
}

func detachFromTerminal() error {
	return unsupportedPlatformOperation("--daemon")
}

func installCmd() *cobra.Command {
	return unsupportedLifecycleCmd("install")
}

func uninstallCmd() *cobra.Command {
	return unsupportedLifecycleCmd("uninstall")
}

func unsupportedLifecycleCmd(name string) *cobra.Command {
	return &cobra.Command{
		Use:   name,
		Short: name + " is unavailable on this platform",
		RunE: func(cmd *cobra.Command, args []string) error {
			return unsupportedPlatformOperation(name)
		},
	}
}
