package quality

import (
	"fmt"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func pythonContractID(contract policy.PythonContract) string {
	return "contract:" + contract.Project + ":" + contract.Kind + ":" + contract.Target
}

func pythonContractOrigin(repo repository.Repository, index int) pythonVultureReferenceOrigin {
	contract := repo.Config.Scope.PythonContracts[index]
	message := fmt.Sprintf("Python contract cannot resolve: /scope/pythonContracts/%d (%s): ", index, contract.Target)
	return pythonVultureReferenceOrigin{Path: pythonVultureConfigPath(repo), Subject: pythonContractID(contract), Check: "policy.pythonContract", Message: message}
}

func pythonVultureContracts(repo repository.Repository, project repository.PythonProject) []pythonVultureContract {
	contracts := []pythonVultureContract{}
	for _, contract := range repo.Config.Scope.PythonContracts {
		if contract.Project == project.Manifest {
			contracts = append(contracts, pythonVultureContract{PythonContract: contract, ID: pythonContractID(contract)})
		}
	}
	return contracts
}
