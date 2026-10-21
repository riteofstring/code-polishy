package testartifact

import (
	"fmt"
	"os"
	"path/filepath"
)

func CleanupCandidates(repositoryRoot string) (removable, kept []string, err error) {
	root := filepath.Join(repositoryRoot, RootName)
	info, err := os.Lstat(root)
	if os.IsNotExist(err) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, nil, fmt.Errorf("test artifact root is not a real directory")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, nil, err
	}
	for _, entry := range entries {
		path := filepath.ToSlash(filepath.Join(RootName, entry.Name()))
		if _, eligible := completedPruneCandidate(root, "", entry); eligible {
			removable = append(removable, path)
		} else {
			kept = append(kept, path)
		}
	}
	return removable, kept, nil
}
