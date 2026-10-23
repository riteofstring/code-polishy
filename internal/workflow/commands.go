package workflow

import (
	"regexp"
	"slices"
	"strings"
)

var gateInvocation = regexp.MustCompile(`^(code-polishy(?:\.exe)?|\./code-polishy|\./code-polishyw(?:\.ps1)?|\.\\code-polishyw\.ps1)[ \t]+merge-gate[ \t]+--base(=|[ \t]+)(.+?)(?:[ \t]+--resume)?$`)
var gateLiteralBase = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._/-]*$`)
var gateScalarBase = regexp.MustCompile(`^"\$[a-zA-Z_][a-zA-Z0-9_]*"$`)

func HasMergeGateCommand(source, shell string) bool {
	return MergeGateCommandIssue(source, shell) == ""
}

func MergeGateCommandIssue(source, shell string) string {
	source = strings.Trim(source, " \t\r\n")
	if strings.ContainsAny(source, "\r\n") {
		return "gate command spans multiple lines; keep one gate invocation and move setup to earlier steps or jobs"
	}
	arguments := gateInvocation.FindStringSubmatch(source)
	if arguments == nil {
		return "expected one dedicated code-polishy or repository-wrapper merge-gate --base REF command, optionally followed by --resume"
	}
	if !gateShellExecutable(shell, arguments[1]) {
		return "gate executable and effective shell must match a supported standard bash, sh, pwsh, powershell, or cmd invocation"
	}
	base := arguments[3]
	if arguments[2] == "=" && (shell == "bash" || shell == "sh") && gateScalarBase.MatchString(base) {
		return ""
	}
	if !gateStaticBase(base, shell) {
		return "gate base or trailing syntax is unsupported; use a literal ref, supported commit-SHA expression, or attached quoted Bash scalar base"
	}
	return ""
}

func gateStaticBase(base, shell string) bool {
	if len(base) >= 2 && base[0] == base[len(base)-1] {
		if base[0] == '"' || base[0] == '\'' && shell != "cmd" {
			base = base[1 : len(base)-1]
		}
	}
	if gateLiteralBase.MatchString(base) {
		return true
	}
	return strings.HasPrefix(base, "${{") && strings.HasSuffix(base, "}}") && workflowCommitExpression(base)
}

func gateShellExecutable(shell, executable string) bool {
	switch shell {
	case "bash", "sh":
		return slices.Contains([]string{"code-polishy", "./code-polishy", "./code-polishyw"}, executable)
	case "pwsh", "powershell":
		return slices.Contains([]string{"code-polishy", "code-polishy.exe", "./code-polishyw.ps1", `.\code-polishyw.ps1`}, executable)
	case "cmd":
		return executable == "code-polishy" || executable == "code-polishy.exe"
	default:
		return false
	}
}

func workflowCommitExpression(expression string) bool {
	expression = strings.TrimSuffix(strings.TrimPrefix(expression, "${{"), "}}")
	for _, reference := range strings.Split(expression, "||") {
		switch strings.ToLower(strings.TrimSpace(reference)) {
		case "github.sha", "github.event.before", "github.event.after",
			"github.event.pull_request.base.sha", "github.event.pull_request.head.sha",
			"github.event.merge_group.base_sha", "github.event.merge_group.head_sha":
		default:
			return false
		}
	}
	return true
}

func JobRunsOnEvent(jobs []Job, id, event string) bool {
	indexed := make(map[string]Job, len(jobs))
	for _, job := range jobs {
		indexed[job.ID] = job
	}
	return jobRunsOnEvent(indexed, id, event, map[string]bool{})
}

func jobRunsOnEvent(jobs map[string]Job, id, event string, results map[string]bool) bool {
	if result, known := results[id]; known {
		return result
	}
	results[id] = false
	job, exists := jobs[id]
	if !exists || !ConditionRunsOnEvent(job.Condition, event) {
		return false
	}
	for _, dependency := range job.Needs {
		if !jobRunsOnEvent(jobs, dependency, event, results) {
			return false
		}
	}
	results[id] = true
	return true
}
