package repository

import (
	"fmt"
	"os"
)

func (repo Repository) ValidateRegularFile(path string) error {
	root, err := os.OpenRoot(repo.Root)
	if err != nil {
		return err
	}
	defer root.Close()
	_, err = repo.containedRegularFileInfo(root, path)
	return err
}

func (repo Repository) WriteRegularFile(path string, data []byte) error {
	root, err := os.OpenRoot(repo.Root)
	if err != nil {
		return err
	}
	defer root.Close()
	expected, err := repo.containedRegularFileInfo(root, path)
	if err != nil {
		return err
	}
	file, err := root.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	actual, err := file.Stat()
	if err != nil || !actual.Mode().IsRegular() || !os.SameFile(expected, actual) {
		return fmt.Errorf("format target changed while opening")
	}
	if err := file.Truncate(0); err != nil {
		return err
	}
	_, err = file.Write(data)
	return err
}
