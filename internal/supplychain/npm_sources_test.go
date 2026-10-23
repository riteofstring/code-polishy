package supplychain

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
)

func TestNPMCustomArtifactAdmission(t *testing.T) {
	t.Parallel()
	lock, err := os.ReadFile("testdata/npm-custom-artifact-lock.json")
	if err != nil {
		t.Fatal(err)
	}
	repo := supplyRepository(t)
	writeSupplyFile(t, repo.Root, "package-lock.json", string(lock))
	registry := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/npm/@fixture/tool" {
			t.Errorf("unexpected metadata request: %s", request.URL.Path)
			http.NotFound(response, request)
			return
		}
		fmt.Fprint(response, `{"name":"@fixture/tool","time":{"1.2.3":"2020-01-01T00:00:00Z"},"versions":{"1.2.3":{"name":"@fixture/tool","version":"1.2.3","dist":{"tarball":"https://registry.example/download/@fixture/tool/1.2.3/artifact"}}}}`)
	}))
	defer registry.Close()
	repo.Config.SupplyChain.NPMRegistryURL = registry.URL + "/npm"
	manifest := nodeManifest{Path: "package.json", Root: ".", Manager: "npm"}
	if findings := checkResolvedNodeReleaseAge(t.Context(), repo, manifest); len(findings) != 0 {
		t.Fatalf("npm-generated artifact URLs rejected: %+v", findings)
	}
	writeSupplyFile(t, repo.Root, "package-lock.json", strings.ReplaceAll(string(lock), "/artifact", "/replacement"))
	if findings := checkResolvedNodeReleaseAge(t.Context(), repo, manifest); len(findings) == 0 {
		t.Fatal("artifact absent from canonical registry metadata was admitted")
	}
}

func TestNPMCustomArtifactHistory(t *testing.T) {
	t.Parallel()
	lock, err := os.ReadFile("testdata/npm-custom-artifact-lock.json")
	if err != nil {
		t.Fatal(err)
	}
	repo := supplyRepository(t)
	writeSupplyFile(t, repo.Root, "package-lock.json", string(lock))
	base := commitReviewInventory(t, repo)
	if changes, err := DependencyChanges(t.Context(), repo, base); err != nil || len(changes) != 0 {
		t.Fatalf("unchanged custom artifact comparison=%+v err=%v", changes, err)
	}
	writeSupplyFile(t, repo.Root, "package-lock.json", strings.ReplaceAll(string(lock), "/artifact", "/replacement"))
	changes, err := DependencyChanges(t.Context(), repo, base)
	if err != nil || len(changes) != 1 || changes[0].Change != "updated" || !strings.Contains(changes[0].From, "/artifact") || !strings.Contains(changes[0].To, "/replacement") {
		t.Fatalf("artifact URL change was lost: %+v err=%v", changes, err)
	}
	writeSupplyFile(t, repo.Root, "package-lock.json", `{"lockfileVersion":3,"packages":{}}`)
	changes, err = DependencyChanges(t.Context(), repo, base)
	if err != nil || len(changes) != 1 || changes[0].Change != "removed" || changes[0].Package != "@fixture/tool" || !strings.Contains(changes[0].From, "https://registry.example/download/@fixture/tool/1.2.3/artifact") {
		t.Fatalf("custom artifact removal=%+v err=%v", changes, err)
	}
}

func TestNPMReviewPreservesPinnedGitSources(t *testing.T) {
	t.Parallel()
	for _, protocol := range []string{"git+https://", "git+ssh://git@", "git+http://"} {
		t.Run(protocol, func(t *testing.T) {
			repo := supplyRepository(t)
			before, after := strings.Repeat("a", 40), strings.Repeat("b", 40)
			origin := protocol + "example.test/team/tool.git#"
			writeSupplyFile(t, repo.Root, "package.json", fmt.Sprintf(`{"packageManager":"npm@10.9.0","dependencies":{"tool":%q}}`, origin+before))
			lock := npmSourceLock("tool", "tool", "1.0.0", origin+before)
			writeSupplyFile(t, repo.Root, "package-lock.json", lock)
			base := commitReviewInventory(t, repo)
			if _, findings := checkPackageJSON(repo, "package.json"); len(findings) != 0 {
				t.Fatalf("pinned Git declaration rejected: %+v", findings)
			}
			if changes, err := DependencyChanges(t.Context(), repo, base); err != nil || len(changes) != 0 {
				t.Fatalf("unchanged pinned Git comparison=%+v err=%v", changes, err)
			}
			packages, err := resolvedNodePackages(t.Context(), repo, nodeManifest{Root: ".", Manager: "npm"})
			if err != nil || len(packages) != 1 || len(registryReleasePackages(packages)) != 0 {
				t.Fatalf("Git source rejected or treated as a registry release: %+v err=%v", packages, err)
			}
			writeSupplyFile(t, repo.Root, "package-lock.json", strings.ReplaceAll(lock, before, after))
			changes, err := DependencyChanges(t.Context(), repo, base)
			if err != nil || len(changes) != 1 || changes[0].Ecosystem != "git" || !strings.Contains(changes[0].From, before) || !strings.Contains(changes[0].To, after) {
				t.Fatalf("Git commit change was lost: %+v err=%v", changes, err)
			}
		})
	}
}

func TestNPMReviewAllowsHistoricalRegistryCleanup(t *testing.T) {
	t.Parallel()
	for _, installed := range []string{"tool", "tool-alias"} {
		t.Run(installed, func(t *testing.T) {
			repo := supplyRepository(t)
			registry := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				fmt.Fprint(response, npmRegistryMetadata("tool", "1.0.0", "https://registry.npmjs.org/tool/-/tool-1.0.0.tgz", `{"1.0.0":"2020-01-01T00:00:00Z"}`))
			}))
			defer registry.Close()
			repo.Config.SupplyChain.NPMRegistryURL = registry.URL
			manifest := nodeManifest{Path: "package.json", Root: ".", Manager: "npm"}
			writeSupplyFile(t, repo.Root, "package.json", `{"packageManager":"npm@10.9.0"}`)
			lock := npmSourceLock(installed, "tool", "1.0.0", "https://former.example/npm/tool/-/tool-1.0.0.tgz")
			writeSupplyFile(t, repo.Root, "package-lock.json", lock)
			base := commitReviewInventory(t, repo)
			if changes, err := DependencyChanges(t.Context(), repo, base); err != nil || len(changes) != 0 {
				t.Fatalf("historical registry comparison=%+v err=%v", changes, err)
			}
			if findings := checkResolvedNodeReleaseAge(t.Context(), repo, manifest); len(findings) == 0 {
				t.Fatal("unconfigured candidate registry was admitted")
			}
			writeSupplyFile(t, repo.Root, "package-lock.json", `{"lockfileVersion":3,"packages":{}}`)
			changes, err := DependencyChanges(t.Context(), repo, base)
			if err != nil || len(changes) != 1 || changes[0].Change != "removed" || changes[0].Package != "tool" || !strings.Contains(changes[0].From, "former.example") {
				t.Fatalf("historical registry removal=%+v err=%v", changes, err)
			}
			if _, err := resolvedNodePackages(t.Context(), repo, nodeManifest{Root: ".", Manager: "npm"}); err != nil {
				t.Fatalf("cleanup candidate rejected: %v", err)
			}
			writeSupplyFile(t, repo.Root, "package-lock.json", strings.ReplaceAll(lock, "https://former.example/npm", "https://registry.npmjs.org"))
			changes, err = DependencyChanges(t.Context(), repo, base)
			if err != nil || len(changes) != 1 || changes[0].Change != "updated" || changes[0].To != "https://registry.npmjs.org/tool/-/tool-1.0.0.tgz@1.0.0" {
				t.Fatalf("registry migration was lost: %+v err=%v", changes, err)
			}
			if findings := checkResolvedNodeReleaseAge(t.Context(), repo, manifest); len(findings) != 0 {
				t.Fatalf("registry migration candidate rejected: %+v", findings)
			}
		})
	}
}

func npmSourceLock(installed, name, version, resolved string) string {
	return fmt.Sprintf(`{"lockfileVersion":3,"packages":{%q:{"name":%q,"version":%q,"resolved":%q}}}`, "node_modules/"+installed, name, version, resolved)
}

func npmRegistryMetadata(name, version, artifact, timestamps string) string {
	return fmt.Sprintf(`{"time":%s,"versions":{%q:{"name":%q,"version":%q,"dist":{"tarball":%q}}}}`, timestamps, version, name, version, artifact)
}

func TestNPMMixedSourcesUseOnlyRegistryPublicationTimes(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	requests := map[string]int{}
	var mutex sync.Mutex
	registry := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		mutex.Lock()
		requests[request.URL.Path]++
		mutex.Unlock()
		fmt.Fprint(response, npmRegistryMetadata("tool", "1.0.0", "https://registry.npmjs.org/tool/-/tool-1.0.0.tgz", `{"1.0.0":"2020-01-01T00:00:00Z"}`))
	}))
	defer registry.Close()
	repo.Config.SupplyChain.NPMRegistryURL = registry.URL
	writeSupplyFile(t, repo.Root, "package-lock.json", fmt.Sprintf(`{"lockfileVersion":3,"packages":{
		"node_modules/alias":{"name":"tool","version":"1.0.0","resolved":"https://registry.npmjs.org/tool/-/tool-1.0.0.tgz"},
		"node_modules/fork":{"name":"tool","version":"1.0.0","resolved":"git+https://example.test/tool.git#%s"}
	}}`, strings.Repeat("a", 40)))
	findings := checkResolvedNodeReleaseAge(t.Context(), repo, nodeManifest{Path: "package.json", Root: ".", Manager: "npm"})
	if len(findings) != 1 || findings[0].Check != "supplyChain.gitSourceCoverage" || findings[0].Severity != policy.FindingWarning {
		t.Fatalf("mixed-source release-age findings=%+v", findings)
	}
	mutex.Lock()
	defer mutex.Unlock()
	if !reflect.DeepEqual(requests, map[string]int{"/tool": 1}) {
		t.Fatalf("registry requests=%v", requests)
	}
}

func TestNPMHistoricalGitRemovalDoesNotAdmitMovingRevisions(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	writeSupplyFile(t, repo.Root, "package-lock.json", npmSourceLock("tool", "tool", "1.0.0", "git+https://example.test/tool.git#main"))
	base := commitReviewInventory(t, repo)
	if _, err := resolvedNodePackages(t.Context(), repo, nodeManifest{Root: ".", Manager: "npm"}); err == nil {
		t.Fatal("moving Git revision admitted")
	}
	writeSupplyFile(t, repo.Root, "package-lock.json", `{"lockfileVersion":3,"packages":{}}`)
	changes, err := DependencyChanges(t.Context(), repo, base)
	if err != nil || len(changes) != 1 || changes[0].Change != "removed" || changes[0].Ecosystem != "git" {
		t.Fatalf("historical Git removal=%+v err=%v", changes, err)
	}
}

func TestNPMFileSourceRemainsLocal(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	writeSupplyFile(t, repo.Root, "vendor/tool.tgz", "local archive fixture")
	writeSupplyFile(t, repo.Root, "package-lock.json", npmSourceLock("tool", "tool", "1.0.0", "file:vendor/tool.tgz"))
	packages, err := resolvedNodePackages(t.Context(), repo, nodeManifest{Root: ".", Manager: "npm"})
	if err != nil || len(packages) != 1 || packages[0].Source.LocalPath != "vendor/tool.tgz" || len(registryReleasePackages(packages)) != 0 {
		t.Fatalf("local source rejected or treated as a registry release: %+v err=%v", packages, err)
	}
}
