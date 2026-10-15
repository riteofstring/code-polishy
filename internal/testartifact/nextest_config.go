package testartifact

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/riteofstring/code-polishy/internal/policy"
)

const NextestConfigName = ".code-polishy-nextest.toml"

func prepareNextest(command policy.Command) error {
	for _, artifact := range command.TestArtifacts {
		if artifact.Type != policy.NextestJUnitArtifact {
			continue
		}
		path, err := containedArtifactPath(command.TestArtifactDirectory, artifact.Path)
		if err != nil {
			return err
		}
		reportPath, _ := json.Marshal(path)
		reportName, _ := json.Marshal(command.TestArtifactSuite)
		storePath, _ := json.Marshal(filepath.Join(command.TestArtifactDirectory, "nextest-state"))
		settings := fmt.Sprintf("[store]\ndir = %s\n[profile.code-polishy]\nretries = 0\nfail-fast = false\n[profile.code-polishy.junit]\npath = %s\nreport-name = %s\n", storePath, reportPath, reportName)
		file, err := os.OpenFile(filepath.Join(command.TestArtifactDirectory, NextestConfigName), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return fmt.Errorf("create Nextest settings: %w", err)
		}
		_, err = file.WriteString(settings)
		return errors.Join(err, file.Close())
	}
	return nil
}
