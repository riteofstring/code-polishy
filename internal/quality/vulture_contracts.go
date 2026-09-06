package quality

import (
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func pythonContractID(contract policy.PythonContract) string {
	return "contract:" + contract.Project + ":" + contract.Kind + ":" + contract.Target
}

func pythonContractOrigin(repo repository.Repository, contract policy.PythonContract) pythonVultureReferenceOrigin {
	return pythonVultureReferenceOrigin{Path: pythonVultureConfigPath(repo), Subject: pythonContractID(contract), Check: "policy.pythonContract", Message: "Python contract cannot resolve: "}
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
