package supplychain

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

const npmAliasLock = `{
  "lockfileVersion": 3,
  "packages": {
    "": {"name":"fixture","version":"1.0.0"},
    "node_modules/string-width-cjs": {"name":"string-width","version":"4.2.3","resolved":"https://registry.npmjs.org/string-width/-/string-width-4.2.3.tgz"},
    "node_modules/parent/node_modules/second-width": {"name":"string-width","version":"4.2.3","resolved":"https://registry.npmjs.org/string-width/-/string-width-4.2.3.tgz"},
    "node_modules/strip-ansi-cjs": {"name":"strip-ansi","version":"6.0.1","resolved":"https://registry.npmjs.org/strip-ansi/-/strip-ansi-6.0.1.tgz"},
    "node_modules/wrap-ansi-cjs": {"name":"wrap-ansi","version":"7.0.0","resolved":"https://registry.npmjs.org/wrap-ansi/-/wrap-ansi-7.0.0.tgz"},
    "node_modules/lru-cache": {"name":"@wolfy1339/lru-cache","version":"11.0.2-patch.1","resolved":"https://registry.npmjs.org/@wolfy1339/lru-cache/-/lru-cache-11.0.2-patch.1.tgz"}
  }
}`

func TestNPMAliasReleaseAgeUsesCanonicalIdentity(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, metadata string
		findings       int
		missing        bool
	}{
		{"aged", `{"4.2.3":"2021-09-23T17:17:21.341Z"}`, 0, false},
		{"missing canonical timestamp", `{"4.2.2":"2021-09-23T17:17:21.341Z"}`, 1, true},
		{"young canonical release", `{"4.2.3":"2026-10-21T00:00:00Z"}`, 1, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := supplyRepository(t)
			writeSupplyFile(t, repo.Root, "package-lock.json", npmAliasLock)
			metadata := map[string]string{
				"/string-width":         npmRegistryMetadata("string-width", "4.2.3", "https://registry.npmjs.org/string-width/-/string-width-4.2.3.tgz", test.metadata),
				"/strip-ansi":           npmRegistryMetadata("strip-ansi", "6.0.1", "https://registry.npmjs.org/strip-ansi/-/strip-ansi-6.0.1.tgz", `{"6.0.1":"2021-09-23T16:34:41.798Z"}`),
				"/wrap-ansi":            npmRegistryMetadata("wrap-ansi", "7.0.0", "https://registry.npmjs.org/wrap-ansi/-/wrap-ansi-7.0.0.tgz", `{"7.0.0":"2020-04-22T16:53:23.889Z"}`),
				"/@wolfy1339/lru-cache": npmRegistryMetadata("@wolfy1339/lru-cache", "11.0.2-patch.1", "https://registry.npmjs.org/@wolfy1339/lru-cache/-/lru-cache-11.0.2-patch.1.tgz", `{"11.0.2-patch.1":"2024-11-05T01:30:31.757Z"}`),
			}
			requests := map[string]int{}
			var mutex sync.Mutex
			registry := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				mutex.Lock()
				requests[request.URL.Path]++
				mutex.Unlock()
				payload, found := metadata[request.URL.Path]
				if !found {
					http.NotFound(response, request)
					return
				}
				fmt.Fprint(response, payload)
			}))
			defer registry.Close()
			repo.Config.SupplyChain.NPMRegistryURL = registry.URL
			packages, err := resolvedNodePackages(t.Context(), repo, nodeManifest{Root: ".", Manager: "npm"})
			if err != nil || len(packages) != 4 {
				t.Fatalf("canonical inventory=%+v err=%v", packages, err)
			}
			wantLocations := []string{"node_modules/parent/node_modules/second-width", "node_modules/string-width-cjs"}
			var locations []string
			for _, item := range packages {
				if item.Name == "string-width" {
					locations = item.Locations
				}
			}
			if !reflect.DeepEqual(locations, wantLocations) {
				t.Fatalf("alias locations were not retained: %+v", packages)
			}
			findings := releaseAgeFindings(t.Context(), repo, packages, time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC))
			if len(findings) != test.findings {
				t.Fatalf("release-age findings=%+v", findings)
			}
			if len(findings) > 0 {
				finding := findings[0]
				if finding.Subject != "string-width@4.2.3" || finding.Path != "package-lock.json" {
					t.Fatalf("wrong release identity: %+v", finding)
				}
				if test.missing && (finding.ReleaseAge != nil || !strings.Contains(finding.Message, "omitted the exact timestamp")) {
					t.Fatalf("missing metadata was accepted: %+v", finding)
				}
				if !test.missing && (finding.ReleaseAge == nil || finding.ReleaseAge.Package != "string-width") {
					t.Fatalf("age threshold was not enforced: %+v", finding)
				}
			}
			mutex.Lock()
			defer mutex.Unlock()
			wantRequests := map[string]int{"/string-width": 1, "/strip-ansi": 1, "/wrap-ansi": 1, "/@wolfy1339/lru-cache": 1}
			if !reflect.DeepEqual(requests, wantRequests) {
				t.Fatalf("registry requests=%v", requests)
			}
		})
	}
}

func TestNPMAliasLockRequiresConsistentSourceIdentity(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name         string
		changes      map[string]string
		publishedURL string
		accepted     bool
	}{
		{"valid alias", nil, "", true},
		{"missing canonical name", map[string]string{"name": ""}, "", false},
		{"missing source", map[string]string{"resolved": ""}, "", false},
		{"different canonical name", map[string]string{"name": "strip-ansi"}, "", false},
		{"different version", map[string]string{"version": "4.2.2"}, "", false},
		{"different registry", map[string]string{"resolved": "https://other.example/string-width/-/string-width-4.2.3.tgz"}, "", false},
		{"encoded path names a different artifact", map[string]string{"resolved": "https://registry.npmjs.org/string-width%2f-/string-width-4.2.3.tgz"}, "", false},
		{"unreadable URL", map[string]string{"resolved": "%"}, "", false},
		{"query", map[string]string{"resolved": "https://registry.npmjs.org/string-width/-/string-width-4.2.3.tgz?other"}, "", false},
		{"credentials", map[string]string{"resolved": "https://user:secret@registry.npmjs.org/string-width/-/string-width-4.2.3.tgz"}, "", false},
		{"registry download endpoint", map[string]string{"resolved": "https://packages.example/download/artifact"}, "https://packages.example/download/artifact", true},
		{"encoded scope", map[string]string{"name": "@scope/string-width", "resolved": "https://registry.npmjs.org/@scope%2fstring-width/-/string-width-4.2.3.tgz"}, "", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			entry := map[string]string{
				"name": "string-width", "version": "4.2.3",
				"resolved": "https://registry.npmjs.org/string-width/-/string-width-4.2.3.tgz",
			}
			for name, value := range test.changes {
				entry[name] = value
			}
			data, err := json.Marshal(map[string]any{"lockfileVersion": 3, "packages": map[string]any{"node_modules/string-width-cjs": entry}})
			if err != nil {
				t.Fatal(err)
			}
			repo := supplyRepository(t)
			publishedName, publishedURL := "string-width", "https://registry.npmjs.org/string-width/-/string-width-4.2.3.tgz"
			if test.publishedURL != "" {
				publishedURL = test.publishedURL
			}
			if test.name == "encoded scope" {
				publishedName, publishedURL = "@scope/string-width", "https://registry.npmjs.org/@scope%2fstring-width/-/string-width-4.2.3.tgz"
			}
			registry := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				fmt.Fprint(response, npmRegistryMetadata(publishedName, "4.2.3", publishedURL, `{"4.2.3":"2021-09-23T17:17:21.341Z"}`))
			}))
			defer registry.Close()
			repo.Config.SupplyChain.NPMRegistryURL = registry.URL
			writeSupplyFile(t, repo.Root, "package-lock.json", string(data))
			findings := checkResolvedNodeReleaseAge(t.Context(), repo, nodeManifest{Path: "package.json", Root: ".", Manager: "npm"})
			if test.accepted {
				if len(findings) != 0 {
					t.Fatalf("valid alias rejected: %+v", findings)
				}
			} else if len(findings) != 1 || !strings.Contains(findings[0].Message, "package-lock.json") || strings.Contains(findings[0].Message, "secret") {
				t.Fatalf("inconsistent alias source accepted or unsafe diagnostic: %+v", findings)
			}
		})
	}
}

func TestDependencyReviewComparesCanonicalNPMAliasVersions(t *testing.T) {
	repo := supplyRepository(t)
	writeSupplyFile(t, repo.Root, "package.json", `{"name":"fixture","packageManager":"npm@10.9.0"}`)
	writeSupplyFile(t, repo.Root, "package-lock.json", npmAliasLock)
	base := commitReviewInventory(t, repo)
	writeSupplyFile(t, repo.Root, "package-lock.json", strings.ReplaceAll(npmAliasLock, "4.2.3", "4.2.4"))
	changes, err := DependencyChanges(t.Context(), repo, base)
	if err != nil || len(changes) != 1 || changes[0].Package != "string-width" || changes[0].From != "https://registry.npmjs.org/string-width/-/string-width-4.2.3.tgz@4.2.3" || changes[0].To != "https://registry.npmjs.org/string-width/-/string-width-4.2.4.tgz@4.2.4" {
		t.Fatalf("alias version change=%+v err=%v", changes, err)
	}
}
