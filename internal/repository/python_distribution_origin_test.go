package repository

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/pythonfacts"
)

func TestPythonDistributionOriginBindsExactGitSource(t *testing.T) {
	commit := strings.Repeat("a", 40)
	for _, repositoryURL := range []string{"https://git.example.test/team/framework.git", "ssh://git@git.example.test/team/private.git"} {
		t.Run(repositoryURL, func(t *testing.T) {
			repo, project, dependency := distributionSourceFixture(t)
			source, err := ParsePythonGitSource(repositoryURL, commit, "packages/framework")
			if err != nil {
				t.Fatal(err)
			}
			dependency.Kind, dependency.Source = "git", source.Identity()
			consumer := policy.PythonDynamicConsumer{Kind: "base", Qualified: "framework.Contract", Member: "run"}
			if _, _, err := repo.pythonReachabilityContract(project, dependency, consumer); err == nil {
				t.Fatal("Git contract accepted without origin")
			}
			origin := fmt.Sprintf(`{"url":%q,"subdirectory":"packages/framework","vcs_info":{"vcs":"git","commit_id":%q,"requested_revision":%q}}`, repositoryURL, commit, commit)
			writeRecordedDistributionOrigin(t, repo, origin)
			identity, proof, err := repo.pythonReachabilityContract(project, dependency, consumer)
			if err != nil || identity == "" || len(proof.Definitions) != 2 {
				t.Fatalf("exact Git contract = %+v, %v", proof, err)
			}
			writeRecordedDistributionOrigin(t, repo, strings.ReplaceAll(origin, commit, strings.Repeat("b", 40)))
			if _, _, err := repo.pythonReachabilityContract(project, dependency, consumer); err == nil {
				t.Fatal("different installed Git commit preserved contract evidence")
			}
		})
	}
}

func TestPythonDistributionOriginRejectsUnsupportedAndAmbiguousInputs(t *testing.T) {
	commit := strings.Repeat("a", 40)
	dependency := PythonPluginDependency{Kind: "git", Source: "git+https://git.example.test/framework.git@" + commit}
	valid := fmt.Sprintf(`{"url":"https://git.example.test/framework.git","vcs_info":{"vcs":"git","commit_id":%q}}`, commit)
	for name, origin := range map[string]string{
		"duplicate key":      strings.Replace(valid, `"vcs":"git"`, `"vcs":"hg","vcs":"git"`, 1),
		"unknown field":      strings.Replace(valid, `"vcs":"git"`, `"vcs":"git","extra":true`, 1),
		"wrong vcs":          strings.Replace(valid, `"vcs":"git"`, `"vcs":"hg"`, 1),
		"short commit":       strings.Replace(valid, commit, "aaaaaaa", 1),
		"wrong repository":   strings.Replace(valid, "framework.git", "other.git", 1),
		"wrong subdirectory": strings.Replace(valid, `"url":`, `"subdirectory":"other","url":`, 1),
		"credentials":        strings.Replace(valid, "https://", "https://secret@", 1),
		"editable":           `{"url":"file:///tmp/framework","dir_info":{"editable":true}}`,
		"excessive nesting":  strings.Repeat("[", 9) + strings.Repeat("]", 9),
		"oversized":          strings.Repeat(" ", 1<<20) + valid,
	} {
		t.Run(name, func(t *testing.T) {
			snapshot := PythonDistributionSources{Origin: &pythonfacts.Input{Source: origin}}
			if err := validatePythonDistributionOrigin(snapshot, dependency); err == nil {
				t.Fatal("invalid origin accepted")
			}
		})
	}
	snapshot := PythonDistributionSources{Origin: &pythonfacts.Input{Source: valid}}
	if err := validatePythonDistributionOrigin(snapshot, PythonPluginDependency{Kind: "registry"}); err == nil {
		t.Fatal("Git installation satisfied registry admission")
	}
}

func TestPythonDistributionSourcesRejectUnrecordedOrigin(t *testing.T) {
	repo, project, dependency := distributionSourceFixture(t)
	writeFile(t, repo.Root, ".venv/lib/python3.12/site-packages/framework-1.0.dist-info/direct_url.json", `{}`)
	if _, err := repo.ReadPythonDistributionSources(project, dependency); err == nil {
		t.Fatal("origin outside RECORD was ignored")
	}
}

func writeRecordedDistributionOrigin(t *testing.T, repo Repository, source string) {
	t.Helper()
	root := ".venv/lib/python3.12/site-packages"
	data, err := os.ReadFile(filepath.Join(repo.Root, root, "framework.py"))
	if err != nil {
		t.Fatal(err)
	}
	writeDistributionRecord(t, repo, root, string(data))
	name := "framework-1.0.dist-info/direct_url.json"
	writeFile(t, repo.Root, root+"/"+name, source)
	record := filepath.Join(repo.Root, root, "framework-1.0.dist-info/RECORD")
	data, err = os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(source))
	data = append(data, []byte(fmt.Sprintf("%s,sha256=%s,%d\n", name, base64.RawURLEncoding.EncodeToString(digest[:]), len(source)))...)
	if err := os.WriteFile(record, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
