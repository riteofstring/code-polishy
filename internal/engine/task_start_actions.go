package engine

func taskStartActions(packet TaskStartPacket) []TaskStartAction {
	actions := []TaskStartAction{
		{Name: "read-workflow", Description: "Read the locked release's workflow before implementation.", Argv: []string{"code-polishy", "docs", "read", "agent-workflows"}},
	}
	if len(packet.RepositoryContext.DesignDocuments)+len(packet.RepositoryContext.Handoffs) > 0 {
		actions = append(actions, TaskStartAction{Name: "read-context", Description: "Read the included design documents and handoffs. Reuse design context until scope, mappings, or relevant documents change."})
	}
	resolution := packet.RepositoryContext.DesignResolution
	if len(resolution.UnmappedModules)+len(resolution.UnmappedPaths) > 0 {
		actions = append(actions, TaskStartAction{Name: "review-design-coverage", Description: "Inspect the reported design mapping gaps and existing rationale. During adoption or consequential design changes, create or update useful rationale and mappings; empty coverage alone does not block routine edits."})
	}
	actions = append(actions,
		TaskStartAction{Name: "implement", Description: "Implement the request within its selected scope and policy; checkpoint task-owned progress at milestones during long work."},
		TaskStartAction{Name: "inspect-verification", Description: "After a coherent change, inspect affected verification without running suites.", Argv: []string{"code-polishy", "test-plan", "--base", packet.TaskBase}},
		TaskStartAction{Name: "verify", Description: "Follow the workflow's event rules: ordinary Markdown needs formatting; source changes need affected exact checks. Supplemental suites require explicit selection."},
		TaskStartAction{Name: "commit", Description: "After required verification, commit remaining task-owned changes unless the caller requested an uncommitted handoff."},
	)
	if packet.Intent.WillBeUsed {
		actions = append(actions,
			TaskStartAction{Name: "review-status", Description: "Inspect required behavior review for the completed candidate.", Argv: []string{"code-polishy", "behavior-review", "status", "--base", packet.TaskBase}},
			TaskStartAction{Name: "complete-reviews", Description: "Complete every selected architecture and behavior review before delivering the task."},
		)
	}
	return append(actions, TaskStartAction{
		Name:        "deliver",
		Description: "Deliver after the event-selected verification and commit. Ordinary task completion and a requested commit do not select a merge gate; run one only at a separately established genuine merge or release checkpoint.",
	})
}
