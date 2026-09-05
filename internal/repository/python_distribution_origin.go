package repository

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"

	policyschema "github.com/riteofstring/code-polishy/schema"
)

var pythonInstalledGitOriginValidator = policyschema.NewValidator(policyschema.ConfigurationBase + "code-polishy-python.schema.json#/$defs/installedGitOrigin")

func (capture pythonDistributionCapture) validateOriginRecord() error {
	name := path.Join(path.Dir(capture.result.Metadata.Path), "direct_url.json")
	_, err := capture.repo.containedRegularFileInfo(capture.root, name)
	if errors.Is(err, os.ErrNotExist) && capture.result.Origin == nil {
		return nil
	}
	if err != nil {
		return err
	}
	if capture.result.Origin == nil {
		return fmt.Errorf("installed distribution origin is absent from RECORD")
	}
	return nil
}

func validatePythonDistributionOrigin(snapshot PythonDistributionSources, dependency PythonPluginDependency) error {
	if dependency.Kind == "registry" && snapshot.Origin == nil {
		return nil
	}
	if dependency.Kind != "git" || snapshot.Origin == nil {
		return fmt.Errorf("installed distribution origin does not match its admitted source kind")
	}
	data := []byte(snapshot.Origin.Source)
	if len(data) > 1<<20 {
		return fmt.Errorf("installed distribution origin exceeds its byte boundary")
	}
	if err := policyschema.ValidateUniqueJSON(data, 8); err != nil {
		return fmt.Errorf("installed distribution origin has invalid or duplicated JSON fields")
	}
	if err := pythonInstalledGitOriginValidator.Validate(data); err != nil {
		return fmt.Errorf("installed distribution origin does not match the Git origin schema")
	}
	return validatePythonDistributionGitOrigin(data, dependency.Source)
}

func validatePythonDistributionGitOrigin(data []byte, expected string) error {
	var origin struct {
		URL          string `json:"url"`
		Subdirectory string `json:"subdirectory"`
		VCS          struct {
			Commit string `json:"commit_id"`
		} `json:"vcs_info"`
	}
	if err := json.Unmarshal(data, &origin); err != nil {
		return err
	}
	source, err := ParsePythonGitSource(origin.URL, origin.VCS.Commit, origin.Subdirectory)
	if err != nil {
		return fmt.Errorf("installed Git origin is not an exact supported source")
	}
	if source.Identity() != expected {
		return fmt.Errorf("installed Git origin differs from the admitted repository, commit, or subdirectory")
	}
	return nil
}
