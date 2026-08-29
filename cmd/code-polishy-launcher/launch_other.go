//go:build !unix && !windows

package main

import "fmt"

func launch(binary string, _ []string) (int, error) {
	return 0, fmt.Errorf("this host cannot execute the installed Code Polishy release at %s", binary)
}
