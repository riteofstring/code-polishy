package repository

import (
	"fmt"

	"github.com/riteofstring/code-polishy/internal/pythonfacts"
)

type PythonLockSourceField struct {
	Name  string
	Value string
}

type PythonLockPackage struct {
	NameInput string
	Name      string
	Version   string
	Source    []PythonLockSourceField
}

type PythonLock struct {
	Version        int
	Revision       int
	RequiresPython string
	Packages       []PythonLockPackage
}

func ParsePythonUVLock(path string, data []byte) (PythonLock, error) {
	python, err := pythonfacts.DefaultInterpreter()
	if err != nil {
		return PythonLock{}, err
	}
	response, err := pythonfacts.Analyze(python, pythonfacts.Request{Locks: []pythonfacts.Input{{Path: path, Source: string(data)}}})
	if err != nil {
		return PythonLock{}, err
	}
	fact := response.Locks[0]
	if fact.Error != "" {
		return PythonLock{}, fmt.Errorf("parse %s: %s", path, fact.Error)
	}
	result := PythonLock{Version: fact.Version, Revision: fact.Revision, RequiresPython: fact.RequiresPython, Packages: make([]PythonLockPackage, 0, len(fact.Packages))}
	for _, item := range fact.Packages {
		if item.NameInput == "" || item.Name == "" {
			return PythonLock{}, fmt.Errorf("parse %s: python-facts returned an invalid package identity", path)
		}
		fields := make([]PythonLockSourceField, 0, len(item.Source))
		for _, field := range item.Source {
			if field.Name == "" {
				return PythonLock{}, fmt.Errorf("parse %s: python-facts returned an invalid package source", path)
			}
			fields = append(fields, PythonLockSourceField{Name: field.Name, Value: field.Value})
		}
		result.Packages = append(result.Packages, PythonLockPackage{NameInput: item.NameInput, Name: item.Name, Version: item.Version, Source: fields})
	}
	return result, nil
}
