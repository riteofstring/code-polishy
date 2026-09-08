package pack

import (
	"encoding/json"
	"errors"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func PlannedExecution(repo repository.Repository, selection repository.Selection, command policy.Command, profile string) (policy.Command, bool, error) {
	request := requestFor(repo, selection, command, profile)
	if len(request.Files) == 0 {
		return policy.Command{}, false, nil
	}
	prepared, _, err := prepareExecution(repo, command, request)
	return prepared, true, err
}

func prepareExecution(repo repository.Repository, command policy.Command, request Request) (policy.Command, Request, error) {
	if err := prepareInputs(repo, &request); err != nil {
		return policy.Command{}, request, err
	}
	prepared, identity, err := runtimeCommand(repo, command, command.Adapter.Runtime)
	if err != nil {
		return policy.Command{}, request, err
	}
	request.Runtime = identity
	data, err := json.Marshal(request)
	if err != nil {
		return policy.Command{}, request, err
	}
	if len(data) > 8<<20 {
		return policy.Command{}, request, errors.New("adapter request exceeds 8 MiB")
	}
	prepared.Stdin = append(data, '\n')
	return prepared, request, nil
}
