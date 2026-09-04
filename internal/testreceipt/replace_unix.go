//go:build !windows

package testreceipt

import "os"

func replaceReceipt(source, destination string) error {
	return os.Rename(source, destination)
}
