package engine

import (
	"context"
	"fmt"
	"slices"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/quality"
	"github.com/riteofstring/code-polishy/internal/repository"
)

type FormatOutcome struct {
	Rewritten int          `json:"rewritten"`
	Unchanged int          `json:"unchanged"`
	Protected int          `json:"protected"`
	Files     []FormatFile `json:"files"`
}

type FormatFile struct {
	Path        string `json:"path"`
	State       string `json:"state"`
	Producer    string `json:"producer,omitempty"`
	StyleExempt bool   `json:"styleExempt"`
}

func (engine *Engine) Format(ctx context.Context, selection repository.Selection) Report {
	files := []string{}
	inventory := repository.GenerationInventory{}
	var err error
	engine.executionPhase(ctx, "format-inventory", func(context.Context) {
		files, err = engine.Repository.AllFiles()
		if err == nil {
			inventory = engine.Repository.InspectGeneration(files)
		}
	})
	if err != nil {
		return engine.formatFailure(selection, "repository", err)
	}
	findings := append([]policy.Finding{}, engine.PolicyModuleFindings...)
	if engine.Repository.HasGeneratedExecutable(selection.Files) {
		findings = append(findings, inventory.Findings...)
	}
	if len(findings) != 0 {
		return engine.withSelection(engine.finish(findings, nil), selection)
	}
	before := map[string]string{}
	engine.executionPhase(ctx, "format-input-snapshot", func(context.Context) {
		before, err = engine.formatSnapshot(selection.Files)
	})
	if err != nil {
		return engine.formatFailure(selection, "inputs", err)
	}
	engine.executionPhase(ctx, "formatters", func(phaseContext context.Context) {
		findings = quality.Format(phaseContext, engine.Repository, selection, engine.Runner)
	})
	outcome := FormatOutcome{}
	protectionFindings := []policy.Finding{}
	engine.executionPhase(ctx, "format-output-verification", func(context.Context) {
		outcome, protectionFindings = engine.formatOutcome(selection.Files, before, inventory)
	})
	findings = append(findings, protectionFindings...)
	report := Report{}
	engine.executionPhase(ctx, "report-assembly", func(context.Context) {
		report = engine.withSelection(engine.finish(findings, nil), selection)
		report.Formatting = &outcome
	})
	return report
}

func (engine *Engine) formatFailure(selection repository.Selection, subject string, err error) Report {
	finding := policy.Finding{Check: "policy.formatEvidence", Path: "repository", Subject: subject, Message: err.Error()}
	return engine.withSelection(engine.finish([]policy.Finding{finding}, nil), selection)
}

func (engine *Engine) formatSnapshot(files []string) (map[string]string, error) {
	before := make(map[string]string, len(files))
	for _, path := range files {
		digest, err := engine.Repository.ContentDigest(path)
		if err != nil {
			return nil, fmt.Errorf("format input %q: %w", path, err)
		}
		before[path] = digest
	}
	return before, nil
}

func (engine *Engine) formatOutcome(files []string, before map[string]string, inventory repository.GenerationInventory) (FormatOutcome, []policy.Finding) {
	outcome := FormatOutcome{Files: []FormatFile{}}
	findings := []policy.Finding{}
	for _, path := range slices.Sorted(slices.Values(files)) {
		file, finding := engine.formatFileOutcome(path, before[path], inventory)
		outcome.Files = append(outcome.Files, file)
		switch file.State {
		case "protected":
			outcome.Protected++
		case "rewritten":
			outcome.Rewritten++
		case "unchanged":
			outcome.Unchanged++
		}
		if finding != nil {
			findings = append(findings, *finding)
		}
	}
	return outcome, findings
}

func (engine *Engine) formatFileOutcome(path, before string, inventory repository.GenerationInventory) (FormatFile, *policy.Finding) {
	file := FormatFile{Path: path, State: "unchanged", StyleExempt: engine.Repository.IsGenerated(path) || engine.Repository.IsData(path)}
	if producer, found := inventory.ProducerFor(path); found {
		file.Producer = producer.Declaration.Name
	}
	after, err := engine.Repository.ContentDigest(path)
	if err != nil {
		file.State = "unavailable"
		return file, &policy.Finding{Check: "policy.formatEvidence", Path: path, Subject: "output", Message: err.Error()}
	}
	if file.StyleExempt {
		file.State = "protected"
		if before != after {
			file.State = "modified-protected"
			return file, &policy.Finding{Check: "policy.generatedWriteProtection", Path: path, Subject: "format", Message: "a formatting command changed protected output; repair the formatter and restore the output through its authoritative source"}
		}
	} else if before != after {
		file.State = "rewritten"
	}
	return file, nil
}

func combineFormatting(left, right *FormatOutcome) *FormatOutcome {
	if right != nil {
		return right
	}
	return left
}
