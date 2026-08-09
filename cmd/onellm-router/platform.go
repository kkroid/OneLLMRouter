package main

import "fmt"

func unsupportedPlatformOperation(operation string) error {
	return fmt.Errorf("%s is unsupported on this platform", operation)
}
