//go:build !unix && !windows

package main

import "fmt"

// Code Polishy publishes releases for Unix hosts only, which release.Host
// enforces before anything is launched, so this host never reaches a release to
// execute.
func launch(binary string, _ []string) (int, error) {
	return 0, fmt.Errorf("this host cannot execute the installed Code Polishy release at %s", binary)
}
