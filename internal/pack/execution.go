package pack

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

const DiscoveryInputDerivation = "validated-pack-discovery-v1"

func PlannedExecutions(repo repository.Repository, selection repository.Selection, command policy.Command, profile string) ([]policy.Command, bool, error) {
	request := requestFor(repo, selection, command, profile)
	if len(request.Files) == 0 && !AdapterSelected(repo, selection, command, profile) {
		return nil, false, nil
	}
	prepared, identities, err := toolchainCommand(repo, command, command.Adapter.Tools)
	if err != nil {
		return nil, true, err
	}
	request.Tools = identities
	if hasProjectDiscovery(command.Adapter) {
		paths, inventoryErr := repo.AllFiles()
		if inventoryErr != nil {
			return nil, true, inventoryErr
		}
		discovery, discoveryErr := prepareDiscoveryRequest(repo, request, command, paths)
		if discoveryErr != nil {
			return nil, true, discoveryErr
		}
		discoveryCommand, encodeErr := commandForRequest(prepared, discovery)
		if encodeErr != nil {
			return nil, true, encodeErr
		}
		followup := prepared
		followup.InputDerivation = DiscoveryInputDerivation
		followup, encodeErr = commandForRequest(followup, request)
		if encodeErr != nil {
			return nil, true, encodeErr
		}
		return []policy.Command{discoveryCommand, followup}, true, nil
	}
	prepared, _, err = prepareExecution(repo, prepared, request)
	if err != nil {
		return nil, true, err
	}
	return []policy.Command{prepared}, true, nil
}

func prepareExecution(repo repository.Repository, command policy.Command, request Request) (policy.Command, Request, error) {
	if err := prepareInputs(repo, &request, command); err != nil {
		return policy.Command{}, request, err
	}
	if request.Tools == nil {
		prepared, identities, err := toolchainCommand(repo, command, command.Adapter.Tools)
		if err != nil {
			return policy.Command{}, request, err
		}
		command = prepared
		request.Tools = identities
	}
	prepared, err := commandForRequest(command, request)
	return prepared, request, err
}

func commandForRequest(command policy.Command, request Request) (policy.Command, error) {
	data, err := json.Marshal(request)
	if err != nil {
		return policy.Command{}, err
	}
	if len(data) > 64<<20 {
		return policy.Command{}, errors.New("adapter request exceeds 64 MiB")
	}
	command.Stdin = append(data, '\n')
	return command, nil
}

func ValidateDiscoveryFollowup(repo repository.Repository, planned, discovery, actual policy.Command, discoveryOutput []byte) error {
	if planned.InputDerivation != DiscoveryInputDerivation || actual.InputDerivation != DiscoveryInputDerivation {
		return errors.New("pack follow-up does not declare validated discovery derivation")
	}
	base, err := decodePlannedRequest(planned.Stdin)
	if err != nil {
		return fmt.Errorf("decode planned pack follow-up: %w", err)
	}
	discoveryRequest, err := decodePlannedRequest(discovery.Stdin)
	if err != nil {
		return fmt.Errorf("decode planned pack discovery: %w", err)
	}
	response, err := parseResponse(discoveryOutput, discoveryRequest)
	if err != nil {
		return fmt.Errorf("validate planned pack discovery: %w", err)
	}
	derived, err := capabilityRequest(base, response)
	if err != nil {
		return fmt.Errorf("derive planned pack follow-up: %w", err)
	}
	if err := prepareInputs(repo, &derived, planned); err != nil {
		return fmt.Errorf("prepare planned pack follow-up: %w", err)
	}
	expected, err := commandForRequest(planned, derived)
	if err != nil {
		return err
	}
	if !bytes.Equal(expected.Stdin, actual.Stdin) {
		return errors.New("pack follow-up request does not match validated discovery")
	}
	return nil
}

func decodePlannedRequest(data []byte) (Request, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	request := Request{}
	if err := decoder.Decode(&request); err != nil {
		return Request{}, err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return Request{}, errors.New("planned request contains more than one JSON value")
	}
	return request, nil
}
