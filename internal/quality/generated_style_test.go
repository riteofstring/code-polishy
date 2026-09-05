package quality

import (
	"slices"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func TestConfiguredStyleSelectionRetainsGeneratedSecurityInputs(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	repo.Config.Checks = []policy.Command{
		{Name: "style", Provides: []string{"format"}, Argv: []string{"style", "--"}, Paths: []string{"app/**"}, PassFiles: true, RunOn: []string{"check", "gate", "format"}},
		{Name: "security", Provides: []string{"security"}, Argv: []string{"security", "--"}, Paths: []string{"app/**"}, PassFiles: true, RunOn: []string{"check", "gate"}},
	}
	files := []string{"app/source.ts", "app/client.generated.ts"}
	for _, path := range files {
		writeQualityFile(t, repo.Root, path, "export {};\n")
	}
	for _, profile := range []string{"check", "gate"} {
		recorder := &recordingQualityRunner{}
		findings := RunCommandsForProfiles(t.Context(), repo, repository.Selection{Files: files}, recorder, profile)
		if len(findings) != 0 {
			t.Fatalf("configured analysis = %+v", findings)
		}
		_, style, styleFound := recorder.command("style")
		_, security, securityFound := recorder.command("security")
		if !styleFound || !securityFound || !slices.Equal(style.Argv, []string{"style", "--", files[0]}) || !slices.Equal(security.Argv, []string{"security", "--", files[0], files[1]}) {
			t.Fatalf("style = %+v, security = %+v", style, security)
		}
	}
}

func TestUnboundedOrMixedStyleCommandsCannotBypassGeneratedCoverage(t *testing.T) {
	t.Parallel()
	for _, passFiles := range []bool{false, true} {
		repo := qualityRepository(t)
		provides := []string{"format"}
		if passFiles {
			provides = append(provides, "security")
		}
		repo.Config.Checks = []policy.Command{{Name: "mixed", Provides: provides, Argv: []string{"mixed", "."}, Paths: []string{"app/**"}, PassFiles: passFiles, RunOn: []string{"check", "format"}}}
		writeQualityFile(t, repo.Root, "app/client.generated.ts", "export {};\n")
		recorder := &recordingQualityRunner{}
		findings := RunCommandsForProfiles(t.Context(), repo, repository.Selection{Files: []string{"app/client.generated.ts"}}, recorder, "check")
		if len(findings) != 1 || findings[0].Check != "policy.generatedStyleCoverage" || len(recorder.commands) != 0 {
			t.Fatalf("unsupported style coverage = %+v, commands = %+v", findings, recorder.commands)
		}
		if _, err := NamedCommands(repo, []string{"mixed"}); err == nil {
			t.Fatal("named execution bypassed generated style coverage")
		}
	}
}

func TestPythonGeneratedStyleExemptionRetainsSemanticAndSecurityDiagnostics(t *testing.T) {
	t.Parallel()
	repo := pythonQualityRepository(t)
	writeQualityFile(t, repo.Root, "pyproject.toml", "[project]\nname = \"example\"\nversion = \"0\"\nrequires-python = \"==3.12.*\"\n")
	for _, path := range []string{"src/app.py", "src/client.generated.py"} {
		writeQualityFile(t, repo.Root, path, "value = 1\n")
	}
	project := pythonQualityProject{project: repository.PythonProject{Root: "."}, sources: []string{"src/app.py", "src/client.generated.py"}}
	output := []byte(`[
  {"code":"E501","filename":"src/app.py","location":{"row":1,"column":1},"message":"line too long"},
  {"code":"E501","filename":"src/client.generated.py","location":{"row":1,"column":1},"message":"line too long"},
  {"code":"I001","filename":"src/client.generated.py","location":{"row":1,"column":1},"message":"import order"},
  {"code":"F821","filename":"src/client.generated.py","location":{"row":1,"column":1},"message":"undefined symbol"},
  {"code":"S307","filename":"src/client.generated.py","location":{"row":1,"column":1},"message":"unsafe eval"},
  {"code":"DTZ001","filename":"src/client.generated.py","location":{"row":1,"column":1},"message":"missing timezone"},
  {"code":"NPY001","filename":"src/client.generated.py","location":{"row":1,"column":1},"message":"deprecated type"},
  {"code":"INP001","filename":"src/client.generated.py","location":{"row":1,"column":1},"message":"namespace package"},
  {"code":"COM818","filename":"src/client.generated.py","location":{"row":1,"column":1},"message":"bare tuple"}
]`)
	findings := pythonQualityRuffLintFindings(repo, project, output, false)
	if len(findings) != 7 {
		t.Fatalf("generated lint diagnostics = %+v", findings)
	}
	for _, finding := range findings {
		if finding.Path == "src/client.generated.py" && (finding.Subject == "E501" || finding.Subject == "I001") {
			t.Fatalf("generated cosmetic diagnostic survived: %+v", finding)
		}
	}
}
