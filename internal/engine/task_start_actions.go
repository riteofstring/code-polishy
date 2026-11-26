package engine

func taskStartActions(packet TaskStartPacket) []TaskStartAction {
	actions := []TaskStartAction{}
	if len(packet.RepositoryContext.DesignDocuments)+len(packet.RepositoryContext.Handoffs) > 0 {
		actions = append(actions, TaskStartAction{Name: "read-context", Description: "Read the selected design documents and handoffs; reuse them while scope and content remain unchanged."})
	}
	if packet.ReliabilityReminder != nil {
		actions = append(actions, TaskStartAction{
			Name: "review-reliability", Description: "Apply the matched end-to-end reliability questions before adding or tightening defensive machinery.",
			Argv: []string{"./code-polishyw", "docs", "read", "end-to-end-reliability"},
		})
	}
	if packet.Intent.WillBeUsed {
		actions = append(actions,
			TaskStartAction{Name: "review-status", Description: "Inspect required behavior review for the completed candidate.", Argv: []string{"./code-polishyw", "behavior-review", "status", "--base", packet.TaskBase}},
		)
	}
	return actions
}

func taskStartWorkflowDocuments(context *RepositoryContext) []string {
	documents := []string{"docs/agent-workflows.md"}
	if len(context.Handoffs) > 0 {
		documents = append(documents, "docs/operational-handoffs.md")
	}
	for _, situation := range context.Situations {
		switch situation {
		case "dependencies":
			documents = append(documents, "docs/policies/supply-chain.md")
		case "adoption", "upgrade":
			documents = append(documents, "docs/ai-adoption.md")
		case "release":
			documents = append(documents, "docs/release-checklist.md")
		}
	}
	return documents
}
