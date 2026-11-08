//go:build !linux

package ownedoutput

import "errors"

func mountGuard(string, string, bool) error {
	return errors.New("build-cache disposal is unsupported on this host; mount and reparse safety require native validation")
}
