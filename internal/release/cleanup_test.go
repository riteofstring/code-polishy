package release

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const thirdDigest = "3333333333333333333333333333333333333333333333333333333333333333"

type cleanupFixture struct {
	prefix string
}

func newCleanupFixture(t *testing.T) cleanupFixture {
	t.Helper()
	prefix, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return cleanupFixture{prefix: prefix}
}

func (fixture cleanupFixture) release(t *testing.T, lock Lock) string {
	t.Helper()
	directory := Directory(fixture.prefix, lock)
	if err := os.MkdirAll(filepath.Join(directory, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "bin", "tool"), []byte("12345"), 0o755); err != nil {
		t.Fatal(err)
	}
	return directory
}

func (fixture cleanupFixture) repository(t *testing.T, lock Lock) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	writeLockFixture(t, filepath.Join(root, LockFilename), lock)
	return root
}

func (fixture cleanupFixture) registry(t *testing.T) []string {
	t.Helper()
	repositories, err := readRepositoryRegistry(fixture.prefix)
	if err != nil {
		t.Fatal(err)
	}
	return repositories
}

func requirePresent(t *testing.T, paths ...string) {
	t.Helper()
	for _, path := range paths {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("%s was removed: %v", path, err)
		}
	}
}

func requireAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("%s remains: %v", path, err)
	}
}

func TestRecordRepositoryRegistersOnlyRepositoriesRunByTheirInstalledRelease(t *testing.T) {
	t.Parallel()
	fixture := newCleanupFixture(t)
	lock := indexedLockFixture("9.9.9", exampleDigest)
	installed := fixture.release(t, lock)
	repository := fixture.repository(t, lock)
	if err := RecordRepository(repository, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if err := RecordRepository(t.TempDir(), installed); err != nil {
		t.Fatal(err)
	}
	requireAbsent(t, repositoryRegistryPath(fixture.prefix))
	for range 2 {
		if err := RecordRepository(repository, installed); err != nil {
			t.Fatal(err)
		}
	}
	if got := fixture.registry(t); !slices.Equal(got, []string{repository}) {
		t.Fatalf("registry = %v", got)
	}
}

func TestRecordRepositoryDropsRepositoriesWhoseLockIsGone(t *testing.T) {
	t.Parallel()
	fixture := newCleanupFixture(t)
	lock := indexedLockFixture("9.9.9", exampleDigest)
	installed := fixture.release(t, lock)
	departed := fixture.repository(t, lock)
	if err := RecordRepository(departed, installed); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(departed, LockFilename)); err != nil {
		t.Fatal(err)
	}
	arrived := fixture.repository(t, lock)
	if err := RecordRepository(arrived, installed); err != nil {
		t.Fatal(err)
	}
	if got := fixture.registry(t); !slices.Equal(got, []string{arrived}) {
		t.Fatalf("registry = %v", got)
	}
}

func TestCleanReleasesKeepsOnlyReleasesThatRecordedRepositoriesLockNow(t *testing.T) {
	t.Parallel()
	fixture := newCleanupFixture(t)
	current := indexedLockFixture("9.9.9", exampleDigest)
	other := indexedLockFixture("9.9.8-beta", otherDigest)
	unused := indexedLockFixture("9.9.7", thirdDigest)
	currentRelease := fixture.release(t, current)
	otherRelease := fixture.release(t, other)
	unusedRelease := fixture.release(t, unused)
	staging := filepath.Join(fixture.prefix, "releases", ".staging-123")
	if err := os.Mkdir(staging, 0o755); err != nil {
		t.Fatal(err)
	}
	repository := fixture.repository(t, current)
	switched := fixture.repository(t, current)
	departed := fixture.repository(t, current)
	for _, root := range []string{switched, departed} {
		if err := RecordRepository(root, currentRelease); err != nil {
			t.Fatal(err)
		}
	}
	writeLockFixture(t, filepath.Join(switched, LockFilename), other)
	if err := os.Remove(filepath.Join(departed, LockFilename)); err != nil {
		t.Fatal(err)
	}

	preview, err := CleanReleases(repository, currentRelease, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Removed) != 1 || preview.Removed[0].Name != filepath.Base(unusedRelease) || preview.ReclaimedBytes() != 5 {
		t.Fatalf("preview = %+v", preview)
	}
	requirePresent(t, unusedRelease)
	if !slices.Contains(fixture.registry(t), departed) {
		t.Fatal("dry run forgot a repository")
	}

	result, err := CleanReleases(repository, currentRelease, false)
	if err != nil {
		t.Fatal(err)
	}
	if want := []KeptRelease{
		{Name: filepath.Base(otherRelease), Repositories: []string{switched}},
		{Name: filepath.Base(currentRelease), Repositories: []string{repository}},
	}; !slices.EqualFunc(result.Kept, want, func(left, right KeptRelease) bool {
		return left.Name == right.Name && slices.Equal(left.Repositories, right.Repositories)
	}) {
		t.Fatalf("kept = %+v, want %+v", result.Kept, want)
	}
	if !slices.Equal(result.ForgottenRepositories, []string{departed}) {
		t.Fatalf("forgotten = %v", result.ForgottenRepositories)
	}
	requirePresent(t, currentRelease, otherRelease, staging)
	requireAbsent(t, unusedRelease)
	if got := fixture.registry(t); !slices.Equal(got, []string{switched}) {
		t.Fatalf("registry = %v", got)
	}
	leftovers, err := filepath.Glob(filepath.Join(fixture.prefix, removalPrefix+"*"))
	if err != nil || len(leftovers) != 0 {
		t.Fatalf("removal staging remains: %v %v", leftovers, err)
	}
}

func TestCleanReleasesFinishesAnInterruptedRemoval(t *testing.T) {
	t.Parallel()
	fixture := newCleanupFixture(t)
	lock := indexedLockFixture("9.9.9", exampleDigest)
	installed := fixture.release(t, lock)
	interrupted := filepath.Join(fixture.prefix, removalPrefix+"abc", "9.9.7-"+thirdDigest)
	if err := os.MkdirAll(interrupted, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := CleanReleases(fixture.repository(t, lock), installed, false); err != nil {
		t.Fatal(err)
	}
	requireAbsent(t, filepath.Dir(interrupted))
}

func uncertainCleanupFixture(t *testing.T) (cleanupFixture, string, string, string) {
	t.Helper()
	fixture := newCleanupFixture(t)
	current := indexedLockFixture("9.9.9", exampleDigest)
	currentRelease := fixture.release(t, current)
	unusedRelease := fixture.release(t, indexedLockFixture("9.9.7", thirdDigest))
	recorded := fixture.repository(t, current)
	if err := RecordRepository(recorded, currentRelease); err != nil {
		t.Fatal(err)
	}
	return fixture, currentRelease, unusedRelease, recorded
}

func TestCleanReleasesRemovesNothingWhenARecordedLockIsUnreadable(t *testing.T) {
	t.Parallel()
	fixture, currentRelease, unusedRelease, recorded := uncertainCleanupFixture(t)
	if err := os.WriteFile(filepath.Join(recorded, LockFilename), []byte("<<<<<<< HEAD\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := CleanReleases(fixture.repository(t, indexedLockFixture("9.9.9", exampleDigest)), currentRelease, false)
	if err == nil || !strings.Contains(err.Error(), recorded) || !strings.Contains(err.Error(), "removed nothing") {
		t.Fatalf("error = %v", err)
	}
	requirePresent(t, unusedRelease)
}

func TestDamagedRegistryIsNeitherReplacedNorTrusted(t *testing.T) {
	t.Parallel()
	fixture, currentRelease, unusedRelease, _ := uncertainCleanupFixture(t)
	damaged := []byte("{")
	if err := os.WriteFile(repositoryRegistryPath(fixture.prefix), damaged, 0o644); err != nil {
		t.Fatal(err)
	}
	repository := fixture.repository(t, indexedLockFixture("9.9.9", exampleDigest))
	if err := RecordRepository(repository, currentRelease); err == nil {
		t.Fatal("recording accepted a damaged registry")
	}
	if after, err := os.ReadFile(repositoryRegistryPath(fixture.prefix)); err != nil || string(after) != string(damaged) {
		t.Fatalf("registry = %q, %v", after, err)
	}
	if _, err := CleanReleases(repository, currentRelease, false); err == nil || !strings.Contains(err.Error(), "repair or remove it") {
		t.Fatalf("error = %v", err)
	}
	requirePresent(t, unusedRelease)
}

func TestCleanReleasesRequiresTheInstalledLockedRelease(t *testing.T) {
	t.Parallel()
	fixture := newCleanupFixture(t)
	lock := indexedLockFixture("9.9.9", exampleDigest)
	fixture.release(t, lock)
	if _, err := CleanReleases(fixture.repository(t, lock), t.TempDir(), false); err == nil || !strings.Contains(err.Error(), "./code-polishyw cleanup") {
		t.Fatalf("error = %v", err)
	}
}

func TestIsReleaseDirectoryNameAcceptsOnlyInstalledReleaseNames(t *testing.T) {
	t.Parallel()
	for name, want := range map[string]bool{
		"9.9.9-" + exampleDigest:      true,
		"1.0.0-rc-1-" + exampleDigest: true,
		".staging-42":                 false,
		"-" + exampleDigest:           false,
		"9.9.9-" + exampleDigest[1:]:  false,
		"9.9.9_" + exampleDigest:      false,
	} {
		if got := isReleaseDirectoryName(name); got != want {
			t.Fatalf("isReleaseDirectoryName(%q) = %v", name, got)
		}
	}
}

func TestConcurrentRecordingKeepsEveryRepository(t *testing.T) {
	t.Parallel()
	fixture := newCleanupFixture(t)
	lock := indexedLockFixture("9.9.9", exampleDigest)
	installed := fixture.release(t, lock)
	repositories := make([]string, 32)
	for index := range repositories {
		repositories[index] = fixture.repository(t, lock)
	}
	errs := make(chan error, len(repositories))
	for _, repository := range repositories {
		go func() { errs <- RecordRepository(repository, installed) }()
	}
	for range repositories {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	if got, want := fixture.registry(t), slices.Sorted(slices.Values(repositories)); !slices.Equal(got, want) {
		t.Fatalf("registry kept %d of %d repositories", len(got), len(want))
	}
}

func TestCleanupDuringRecordingKeepsEveryRecordedRepository(t *testing.T) {
	t.Parallel()
	fixture := newCleanupFixture(t)
	lock := indexedLockFixture("9.9.9", exampleDigest)
	installed := fixture.release(t, lock)
	departed := fixture.repository(t, lock)
	if err := RecordRepository(departed, installed); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(departed, LockFilename)); err != nil {
		t.Fatal(err)
	}
	repositories := make([]string, 16)
	for index := range repositories {
		repositories[index] = fixture.repository(t, lock)
	}
	errs := make(chan error, len(repositories)+1)
	go func() {
		_, err := CleanReleases(repositories[0], installed, false)
		errs <- err
	}()
	for _, repository := range repositories {
		go func() { errs <- RecordRepository(repository, installed) }()
	}
	for range len(repositories) + 1 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	if got := fixture.registry(t); !slices.Equal(got, slices.Sorted(slices.Values(repositories))) {
		t.Fatalf("registry = %v", got)
	}
}
