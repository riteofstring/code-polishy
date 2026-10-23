package engine

import (
	"slices"
	"testing"

	"github.com/riteofstring/code-polishy/internal/gaterun"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
	testpolicy "github.com/riteofstring/code-polishy/internal/testing"
)

func TestCargoSupplyChainRunsTheOnlineGatePlanAtEveryMergeLevel(t *testing.T) {
	t.Parallel()
	for _, level := range []string{testpolicy.MergeLevelRecommended, testpolicy.MergeLevelFull} {
		t.Run(level, func(t *testing.T) {
			engine := cargoSupplyEngine(t)
			plan, err := engine.appendMergeGateSupplyChainCommands(MergeGateExecutionPlan{Level: level})
			if err != nil {
				t.Fatal(err)
			}
			if len(plan.Commands) != 4 || plan.Commands[3].Category != gaterun.SupplyChain {
				t.Fatalf("Cargo gate plan = %+v", plan.Commands)
			}
			boundary := &mergeGatePlannedRunner{root: engine.Repository.Root, delegate: runner.OSRunner{}, expected: plan.Commands}
			engine.Runner = boundary
			report, err := engine.SupplyChain(t.Context(), false)
			if err != nil || boundary.next != len(plan.Commands) {
				t.Fatalf("Cargo execution differs from the gate plan: next=%d, report=%+v, error=%v", boundary.next, report, err)
			}
			for _, finding := range report.Findings {
				if slices.Contains([]string{"supplyChain.cargoLock", "policy.rustProject", "policy.tool"}, finding.Check) {
					t.Errorf("valid Cargo workspace failed supply-chain verification: %+v", finding)
				}
			}
		})
	}
}

func TestCargoSupplyChainRejectsDriftInThePublicEngine(t *testing.T) {
	t.Parallel()
	engine := cargoSupplyEngine(t)
	writeEngineFile(t, engine.Repository.Root, "Cargo.lock", "version=4\n[[package]]\nname='app'\nversion='0.9.0'\n", 0o600)
	report, err := engine.SupplyChain(t.Context(), true)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(report.Findings, func(finding policy.Finding) bool {
		return finding.Check == "supplyChain.cargoLock" && finding.Path == "Cargo.lock"
	}) {
		t.Fatalf("stale Cargo lock passed public supply-chain verification: %+v", report.Findings)
	}
}

func cargoSupplyEngine(t *testing.T) *Engine {
	t.Helper()
	root := t.TempDir()
	writeEngineFile(t, root, "Cargo.toml", "[package]\nname='app'\nversion='1.0.0'\nedition='2021'\n", 0o600)
	writeEngineFile(t, root, "Cargo.lock", "version=4\n[[package]]\nname='app'\nversion='1.0.0'\n", 0o600)
	writeEngineFile(t, root, "src/lib.rs", "pub fn app() {}\n", 0o600)
	return &Engine{Repository: repository.Repository{Root: root, PolicyRoot: enginePolicyRoot(t)}, Runner: runner.OSRunner{}}
}
