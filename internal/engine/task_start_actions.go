package engine

import "github.com/riteofstring/code-polishy/internal/policy"

func taskStartActions(packet TaskStartPacket) []TaskStartAction {
	actions := []TaskStartAction{
		{Name: "read-workflow", Description: "Read the locked release's workflow before implementation.", Argv: []string{"code-polishy", "docs", "read", "agent-workflows"}},
	}
	if len(packet.RepositoryContext.DesignDocuments)+len(packet.RepositoryContext.Handoffs) > 0 {
		actions = append(actions, TaskStartAction{Name: "read-context", Description: "Read the current design documents and operational handoffs included in this packet before their associated work."})
	}
	actions = append(actions,
		TaskStartAction{Name: "implement", Description: "Implement the captured request within its selected scope and configured policy."},
		TaskStartAction{Name: "inspect-verification", Description: "After a coherent change, inspect affected verification without running suites.", Argv: []string{"code-polishy", "test-plan", "--base", packet.Capture.Commit}},
		TaskStartAction{Name: "verify", Description: "Follow the workflow's event rules: ordinary Markdown needs formatting; source changes need affected exact checks. Supplemental suites require explicit selection."},
		TaskStartAction{Name: "commit", Description: "Commit the verified task-owned candidate unless the caller requested an uncommitted handoff."},
		TaskStartAction{Name: "review-status", Description: "Inspect required behavior review for the completed candidate.", Argv: []string{"code-polishy", "behavior-review", "status", "--base", packet.Capture.Commit}},
		TaskStartAction{Name: "complete-reviews", Description: "Complete every selected architecture and behavior review before final delivery."},
	)
	gate := "Resolve the merge target and run one base-aware merge gate for the final candidate."
	if packet.FinalGateOwner == policy.FinalGateOwnerCI {
		gate = "Resolve the merge target and use the checked-in CI workflow for the final merge gate; do not duplicate it locally without an explicit request."
	}
	return append(actions, TaskStartAction{Name: "final-gate", Description: gate})
}
