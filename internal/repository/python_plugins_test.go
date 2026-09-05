package repository

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPythonPluginDependenciesBindCurrentManifestAndAuthoritativeLock(t *testing.T) {
	t.Parallel()
	repo, project := pluginDependencyRepository(t, "plug-dist==1.0", "1.0.0", "registry = 'https://packages.example.test/simple'")
	first, err := repo.PythonPluginDependencies(project, []string{"plug-dist", "plug-dist"})
	if err != nil || len(first.Dependencies) != 1 || first.Dependencies[0].Error != "" || first.Dependencies[0].Version != "1.0.0" || first.Dependencies[0].Kind != "registry" || first.Lock != "apps/api/uv.lock" {
		t.Fatalf("direct plug-in dependency did not match: %+v, %v", first, err)
	}
	writeFile(t, repo.Root, "uv.lock", "not a lock")
	unchanged, err := repo.PythonPluginDependencies(project, []string{"plug-dist"})
	if err != nil || unchanged.LockSHA256 != first.LockSHA256 {
		t.Fatalf("unrelated root lock changed project admission: %+v, %v", unchanged, err)
	}
	writeFile(t, repo.Root, project.Manifest, pluginDependencyManifest("plug-dist==2.0"))
	changed, err := repo.PythonPluginDependencies(project, []string{"plug-dist"})
	if err != nil || changed.ManifestSHA256 == first.ManifestSHA256 || changed.Dependencies[0].Error == "" {
		t.Fatalf("stale project inventory concealed current dependency edit: %+v, %v", changed, err)
	}
	writeFile(t, repo.Root, first.Lock, pluginDependencyLock("2.0", "registry = 'https://packages.example.test/simple'"))
	repaired, err := repo.PythonPluginDependencies(project, []string{"plug-dist"})
	if err != nil || repaired.LockSHA256 == first.LockSHA256 || repaired.Dependencies[0].Error != "" || repaired.Dependencies[0].Version != "2.0" {
		t.Fatalf("current lock repair was not admitted: %+v, %v", repaired, err)
	}
}

func TestPythonPluginDependenciesSupportExactPrivateGitCommits(t *testing.T) {
	t.Parallel()
	commit := strings.Repeat("a", 40)
	requirement := "plug-dist @ git+ssh://git@private.example.test/team/plugins.git@" + commit + "#subdirectory=python/plugin"
	locked := "git = 'ssh://git@private.example.test/team/plugins.git?rev=" + commit + "&subdirectory=python%2Fplugin#" + commit + "'"
	repo, project := pluginDependencyRepository(t, requirement, "1.0.0", locked)
	result, err := repo.PythonPluginDependencies(project, []string{"plug-dist"})
	if err != nil || len(result.Dependencies) != 1 || result.Dependencies[0].Error != "" || result.Dependencies[0].Kind != "git" || result.Dependencies[0].Source != "git+ssh://git@private.example.test/team/plugins.git@"+commit+"#subdirectory=python/plugin" {
		t.Fatalf("exact private Git commit did not acquire dependency identity: %+v, %v", result, err)
	}
	for name, source := range map[string]string{
		"changed commit":        strings.ReplaceAll(locked, commit, strings.Repeat("b", 40)),
		"changed repository":    strings.ReplaceAll(locked, "/team/plugins.git", "/other/plugins.git"),
		"changed subdirectory":  strings.ReplaceAll(locked, "python%2Fplugin", "other"),
		"tag reference":         strings.Replace(locked, "?rev="+commit, "?tag=v1", 1),
		"conflicting revisions": locked + ", commit = '" + strings.Repeat("b", 40) + "'",
		"mixed registry":        locked + ", registry = 'https://pypi.org/simple'",
	} {
		t.Run(name, func(t *testing.T) {
			writeFile(t, repo.Root, "apps/api/uv.lock", pluginDependencyLock("1.0.0", source))
			assertPythonPluginDependencyRejected(t, repo, project)
		})
	}
}

func TestPythonPluginDependenciesRequireDirectExactAdmittedSources(t *testing.T) {
	t.Parallel()
	for name, fixture := range map[string]struct{ requirement, version, source string }{
		"credentialed registry":   {"plug-dist==1.0", "1.0", "registry = 'https://user:password@packages.example.test/simple'"},
		"moving registry query":   {"plug-dist==1.0", "1.0", "registry = 'https://packages.example.test/simple?token=secret'"},
		"local registry":          {"plug-dist==1.0", "1.0", "registry = 'file:///tmp/packages'"},
		"transitive only":         {"other-dist==1.0", "1.0", "registry = 'https://pypi.org/simple'"},
		"range":                   {"plug-dist>=1.0", "1.0", "registry = 'https://pypi.org/simple'"},
		"wildcard":                {"plug-dist==1.*", "1.0", "registry = 'https://pypi.org/simple'"},
		"different version":       {"plug-dist==1.0", "2.0", "registry = 'https://pypi.org/simple'"},
		"different local version": {"plug-dist==1.0+private", "1.0+other", "registry = 'https://pypi.org/simple'"},
		"local lock shadow":       {"plug-dist==1.0", "1.0", "editable = 'local/plugin'"},
		"mixed source":            {"plug-dist==1.0", "1.0", "registry = 'https://pypi.org/simple', directory = 'local/plugin'"},
		"unsupported URL":         {"plug-dist @ https://packages.example.test/plugin.whl", "1.0", "url = 'https://packages.example.test/plugin.whl'"},
		"historical tag":          {"plug-dist @ git+https://git.example.test/team/plugin.git@v1", "1.0", "git = 'https://git.example.test/team/plugin.git?tag=v1#" + strings.Repeat("a", 40) + "'"},
	} {
		t.Run(name, func(t *testing.T) {
			repo, project := pluginDependencyRepository(t, fixture.requirement, fixture.version, fixture.source)
			assertPythonPluginDependencyRejected(t, repo, project)
		})
	}
}

func TestPythonPluginDependenciesRejectBuildAndDevelopmentOnlyDeclarations(t *testing.T) {
	t.Parallel()
	for name, group := range map[string]string{
		"build":       "[build-system]\nrequires = ['plug-dist==1.0']\nbuild-backend = 'plug_dist.backend'\n",
		"development": "[dependency-groups]\ndev = ['plug-dist==1.0']\n",
	} {
		t.Run(name, func(t *testing.T) {
			repo, project := pluginDependencyRepository(t, "other==1.0", "1.0", "registry = 'https://pypi.org/simple'")
			writeFile(t, repo.Root, project.Manifest, pluginDependencyManifest("other==1.0")+group)
			assertPythonPluginDependencyRejected(t, repo, project)
		})
	}
}

func TestPythonPluginDependenciesRejectUnownedLockSubstitutions(t *testing.T) {
	t.Parallel()
	for name, change := range map[string]func(*testing.T, Repository, PythonProject){
		"missing lock": func(t *testing.T, repo Repository, _ PythonProject) {
			if err := os.Remove(filepath.Join(repo.Root, "apps/api/uv.lock")); err != nil {
				t.Fatal(err)
			}
		},
		"symlink lock": func(t *testing.T, repo Repository, _ PythonProject) {
			outside := t.TempDir()
			writeFile(t, outside, "uv.lock", pluginDependencyLock("1.0", "registry = 'https://pypi.org/simple'"))
			if err := os.Remove(filepath.Join(repo.Root, "apps/api/uv.lock")); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join(outside, "uv.lock"), filepath.Join(repo.Root, "apps/api/uv.lock")); err != nil {
				t.Fatal(err)
			}
		},
		"unsupported version": func(t *testing.T, repo Repository, _ PythonProject) {
			writeFile(t, repo.Root, "apps/api/uv.lock", "version = 99\n")
		},
		"malformed lock": func(t *testing.T, repo Repository, _ PythonProject) {
			writeFile(t, repo.Root, "apps/api/uv.lock", "[not toml")
		},
	} {
		t.Run(name, func(t *testing.T) {
			repo, project := pluginDependencyRepository(t, "plug-dist==1.0", "1.0", "registry = 'https://pypi.org/simple'")
			change(t, repo, project)
			if result, err := repo.PythonPluginDependencies(project, []string{"plug-dist"}); err == nil {
				t.Fatalf("invalid lock acquired dependency evidence: %+v", result)
			}
		})
	}
}

func assertPythonPluginDependencyRejected(t *testing.T, repo Repository, project PythonProject) {
	t.Helper()
	result, err := repo.PythonPluginDependencies(project, []string{"plug-dist"})
	if err != nil || len(result.Dependencies) != 1 || result.Dependencies[0].Error == "" || result.Dependencies[0].Source != "" {
		t.Fatalf("invalid dependency acquired admission: %+v, %v", result, err)
	}
}

func pluginDependencyRepository(t *testing.T, requirement, version, source string) (Repository, PythonProject) {
	t.Helper()
	repo := Repository{Root: t.TempDir()}
	project := PythonProject{Manifest: "apps/api/pyproject.toml", Root: "apps/api"}
	writeFile(t, repo.Root, project.Manifest, pluginDependencyManifest(requirement))
	writeFile(t, repo.Root, "apps/api/uv.lock", pluginDependencyLock(version, source))
	return repo, project
}

func pluginDependencyManifest(requirement string) string {
	return "[project]\nname = 'consumer'\nversion = '1.0'\ndependencies = ['" + requirement + "']\n"
}
func pluginDependencyLock(version, source string) string {
	return "version = 1\n[[package]]\nname = 'plug-dist'\nversion = '" + version + "'\nsource = {" + source + "}\n"
}
