package supplychain

import (
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	workflowfacts "github.com/riteofstring/code-polishy/internal/workflow"
	"mvdan.cc/sh/v3/syntax"
)

func securityMonitoringFindings(repo repository.Repository, files []string) []policy.Finding {
	if !repo.Config.SupplyChain.RecurringSecurityMonitoring || !dependencyGraphPresent(repo, files) || hasSecurityMonitoringProvider(repo.Config.Checks) {
		return nil
	}
	workflows := securityWorkflowPaths(files)
	if hasWeeklySecurityWorkflow(repo, workflows) {
		return nil
	}
	message := "dependency graphs require an online code-polishy supply-chain scan at least weekly"
	if len(workflows) == 0 {
		message += "; add a scheduled CI workflow or a security-monitoring provider"
	} else {
		message += "; no GitHub workflow combines a weekly-or-faster schedule with an online supply-chain command"
	}
	return []policy.Finding{{
		Check: "policy.securityMonitoring", Path: policy.ConfigFilename, Subject: "weekly-online-scan", Message: message,
	}}
}

func securityWorkflowPaths(files []string) []string {
	workflows := []string{}
	for _, path := range files {
		workflowPath := strings.HasPrefix(path, ".github/workflows/")
		workflowExtension := slices.Contains([]string{".yml", ".yaml"}, filepath.Ext(path))
		if workflowPath && workflowExtension {
			workflows = append(workflows, path)
		}
	}
	return workflows
}

func hasWeeklySecurityWorkflow(repo repository.Repository, workflows []string) bool {
	for _, path := range workflows {
		data, err := repo.Read(path)
		if err != nil {
			continue
		}
		facts, parseErr := workflowfacts.Parse(path, data)
		if parseErr == nil && workflowFactsRunWeeklySecurity(facts) {
			return true
		}
	}
	return false
}

func dependencyGraphPresent(repo repository.Repository, files []string) bool {
	if slices.Contains(repo.Config.Project.Capabilities, "custom-dependencies") {
		return true
	}
	for _, module := range repo.Config.Modules {
		if slices.Contains(module.Capabilities, "custom-dependencies") {
			return true
		}
	}
	for _, path := range files {
		name := filepath.Base(path)
		if dependencyEcosystem(repo, path) != "" {
			return true
		}
		if slices.Contains([]string{"go.sum", "package-lock.json", "npm-shrinkwrap.json", "pnpm-lock.yaml", "uv.lock"}, name) {
			return true
		}
	}
	return false
}

func hasSecurityMonitoringProvider(commands []policy.Command) bool {
	for _, command := range commands {
		if slices.Contains(command.Provides, "security-monitoring") && slices.Contains(command.RunOn, "security") {
			return true
		}
	}
	return false
}

func workflowRunsWeeklySecurity(contents string) bool {
	facts, err := workflowfacts.Parse("workflow.yml", []byte(contents))
	return err == nil && workflowFactsRunWeeklySecurity(facts)
}

func workflowFactsRunWeeklySecurity(facts workflowfacts.Facts) bool {
	hasSchedule := false
	for _, schedule := range facts.Schedules {
		if cronRunsAtLeastWeekly(schedule.Cron) {
			hasSchedule = true
			break
		}
	}
	if !hasSchedule {
		return false
	}
	for _, job := range facts.Jobs {
		if !workflowJobRunsOnSchedule(facts.Jobs, job.ID, map[string]bool{}) {
			continue
		}
		for _, step := range job.Steps {
			if step.RunsOnSchedule && onlineSecurityCommand(step.Run) {
				return true
			}
		}
	}
	return false
}

func workflowJobRunsOnSchedule(jobs []workflowfacts.Job, id string, visiting map[string]bool) bool {
	if visiting[id] {
		return false
	}
	var selected *workflowfacts.Job
	for index := range jobs {
		if jobs[index].ID == id {
			selected = &jobs[index]
			break
		}
	}
	if selected == nil || !selected.RunsOnSchedule {
		return false
	}
	visiting[id] = true
	defer delete(visiting, id)
	for _, dependency := range selected.Needs {
		if !workflowJobRunsOnSchedule(jobs, dependency, visiting) {
			return false
		}
	}
	return true
}

func onlineSecurityCommand(source string) bool {
	parsed, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(source), "")
	if err != nil {
		return false
	}
	found := false
	syntax.Walk(parsed, func(node syntax.Node) bool {
		call, ok := node.(*syntax.CallExpr)
		if !ok || len(call.Args) < 2 {
			return !found
		}
		arguments := make([]string, len(call.Args))
		for index, word := range call.Args {
			arguments[index] = word.Lit()
			if arguments[index] == "" {
				return !found
			}
		}
		executable := strings.TrimSuffix(strings.ToLower(filepath.Base(strings.ReplaceAll(arguments[0], "\\", "/"))), ".exe")
		if executable != "code-polishy" {
			return !found
		}
		switch arguments[1] {
		case "gate":
			found = true
		case "supply-chain":
			found = true
			for _, argument := range arguments[2:] {
				if strings.HasPrefix(argument, "--offline") {
					found = false
					break
				}
			}
		}
		return !found
	})
	return found
}

func cronRunsAtLeastWeekly(expression string) bool {
	fields := strings.Fields(expression)
	if len(fields) != 5 || !validNumericCronField(fields[0], 0, 59) || !validNumericCronField(fields[1], 0, 23) || fields[3] != "*" {
		return false
	}
	dayOfMonth, dayOfWeek := fields[2], fields[4]
	if dayOfMonth == "*" {
		return validWeeklyDayOfWeek(dayOfWeek)
	}
	if dayOfWeek != "*" || !strings.HasPrefix(dayOfMonth, "*/") {
		return false
	}
	days, err := strconv.Atoi(strings.TrimPrefix(dayOfMonth, "*/"))
	return err == nil && days >= 1 && days <= 7
}

func validNumericCronField(value string, minimum, maximum int) bool {
	for _, part := range strings.Split(value, ",") {
		base, step, hasStep := strings.Cut(part, "/")
		if hasStep {
			parsed, err := strconv.Atoi(step)
			if err != nil || parsed < 1 {
				return false
			}
		}
		if base == "*" {
			continue
		}
		endpoints := strings.Split(base, "-")
		if len(endpoints) > 2 {
			return false
		}
		for _, endpoint := range endpoints {
			parsed, err := strconv.Atoi(endpoint)
			if err != nil || parsed < minimum || parsed > maximum {
				return false
			}
		}
	}
	return value != ""
}

func validWeeklyDayOfWeek(value string) bool {
	if value == "" || strings.ContainsAny(value, "#L") {
		return false
	}
	for _, part := range strings.Split(value, ",") {
		if !validCronDayPart(part) {
			return false
		}
	}
	return true
}

func validCronDayPart(value string) bool {
	if value == "*" {
		return true
	}
	base, step, hasStep := strings.Cut(value, "/")
	if hasStep {
		parsed, err := strconv.Atoi(step)
		if err != nil || parsed < 1 {
			return false
		}
	}
	endpoints := strings.Split(base, "-")
	if len(endpoints) > 2 {
		return false
	}
	for _, endpoint := range endpoints {
		normalized := strings.ToUpper(endpoint)
		if slices.Contains([]string{"SUN", "MON", "TUE", "WED", "THU", "FRI", "SAT"}, normalized) {
			continue
		}
		day, err := strconv.Atoi(endpoint)
		if err != nil || day < 0 || day > 7 {
			return false
		}
	}
	return true
}
