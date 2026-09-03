package supplychain

import (
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func securityMonitoringFindings(repo repository.Repository, files []string) []policy.Finding {
	if !repo.Config.SupplyChain.RecurringSecurityMonitoring || !dependencyGraphPresent(repo, files) || hasSecurityMonitoringProvider(repo.Config.Checks) {
		return nil
	}
	workflows := []string{}
	for _, path := range files {
		if strings.HasPrefix(path, ".github/workflows/") && slices.Contains([]string{".yml", ".yaml"}, filepath.Ext(path)) {
			workflows = append(workflows, path)
		}
	}
	for _, path := range workflows {
		data, err := repo.Read(path)
		if err == nil && workflowRunsWeeklySecurity(string(data)) {
			return nil
		}
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
	state := workflowSecurityState{onIndent: -1, scheduleIndent: -1, runBlockIndent: -1}
	for _, raw := range strings.Split(contents, "\n") {
		line := strings.TrimSpace(strings.SplitN(raw, "#", 2)[0])
		if line == "" {
			continue
		}
		indent := len(raw) - len(strings.TrimLeft(raw, " "))
		state.observeSchedule(line, indent)
		state.observeRun(line, indent)
	}
	return state.hasSchedule && state.hasOnlineScan
}

type workflowSecurityState struct {
	hasSchedule    bool
	hasOnlineScan  bool
	onIndent       int
	scheduleIndent int
	runBlockIndent int
}

func (state *workflowSecurityState) observeSchedule(line string, indent int) {
	switch {
	case line == "on:":
		state.onIndent = indent
		state.scheduleIndent = -1
	case state.onIndent >= 0 && indent <= state.onIndent:
		state.onIndent = -1
		state.scheduleIndent = -1
	case state.onIndent >= 0 && line == "schedule:":
		state.scheduleIndent = indent
	case state.scheduleIndent >= 0 && indent <= state.scheduleIndent:
		state.scheduleIndent = -1
	}
	if state.scheduleIndent < 0 {
		return
	}
	cron, found := workflowCron(line)
	if found && cronRunsAtLeastWeekly(cron) {
		state.hasSchedule = true
	}
}

func (state *workflowSecurityState) observeRun(line string, indent int) {
	if state.runBlockIndent >= 0 && indent > state.runBlockIndent && onlineSecurityCommand(line) {
		state.hasOnlineScan = true
	}
	if state.runBlockIndent >= 0 && indent <= state.runBlockIndent {
		state.runBlockIndent = -1
	}
	value, run := workflowRunValue(line)
	if !run {
		return
	}
	if multilineWorkflowRun(value) {
		state.runBlockIndent = indent
		return
	}
	if onlineSecurityCommand(value) {
		state.hasOnlineScan = true
	}
}

func multilineWorkflowRun(value string) bool {
	return value == "|" || value == ">" || strings.HasPrefix(value, "|-") || strings.HasPrefix(value, ">-")
}

func workflowRunValue(line string) (string, bool) {
	if strings.HasPrefix(line, "- run:") {
		return strings.TrimSpace(strings.TrimPrefix(line, "- run:")), true
	}
	if strings.HasPrefix(line, "run:") {
		return strings.TrimSpace(strings.TrimPrefix(line, "run:")), true
	}
	return "", false
}

func workflowCron(line string) (string, bool) {
	if strings.HasPrefix(line, "- cron:") {
		line = strings.TrimSpace(strings.TrimPrefix(line, "- cron:"))
	} else if strings.HasPrefix(line, "cron:") {
		line = strings.TrimSpace(strings.TrimPrefix(line, "cron:"))
	} else {
		return "", false
	}
	line = strings.Trim(line, `"'`)
	return line, line != ""
}

func onlineSecurityCommand(line string) bool {
	line = strings.ToLower(line)
	if !strings.Contains(line, "code-polishy") {
		return false
	}
	if strings.Contains(line, "code-polishy gate") {
		return true
	}
	return strings.Contains(line, "code-polishy supply-chain") && !strings.Contains(line, "--offline")
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
