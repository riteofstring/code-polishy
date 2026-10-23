package supplychain

import (
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
)

func SecurityScanInputs(command policy.Command) []string {
	if !strings.HasPrefix(command.Name, "pnpm-audit-") || !command.SealedEnvironment || len(command.Paths) != 1 {
		return nil
	}
	return command.Paths
}
