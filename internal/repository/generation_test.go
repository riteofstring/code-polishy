package repository

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/riteofstring/code-polishy/internal/policy"
)

func TestGenerationInventoryResolvesOwnershipWithoutRunningCommands(t *testing.T) {
	t.Parallel()
	repo, files := generationRepository(t)
	inventory := repo.InspectGeneration(files)
	if len(inventory.Findings) != 0 || len(inventory.Producers) != 1 {
		t.Fatalf("inventory = %+v", inventory)
	}
	producer, found := inventory.ProducerFor("python_pkg/client.generated.ts")
	if !found || producer.Declaration.Name != "client" || !slices.Equal(producer.Inputs, []string{"frontend/schema.py"}) || !slices.Equal(producer.Outputs, []string{"python_pkg/client.generated.ts"}) {
		t.Fatalf("resolved producer = %+v, found = %t", producer, found)
	}
	if _, err := os.Stat(filepath.Join(repo.Root, "executed")); !os.IsNotExist(err) {
		t.Fatalf("producer declaration executed a command: %v", err)
	}
	slices.Reverse(files)
	if reordered := repo.InspectGeneration(files); !reflect.DeepEqual(inventory, reordered) {
		t.Fatalf("file order changed generation ownership: %+v", reordered)
	}
}

func TestGenerationInventoryRejectsInvalidCurrentOwnership(t *testing.T) {
	t.Parallel()
	for name, change := range map[string]func(*Repository, *[]string){
		"unowned": func(repo *Repository, _ *[]string) { repo.Config.Generation.Producers = nil },
		"stale outputs": func(repo *Repository, _ *[]string) {
			repo.Config.Generation.Producers[0].Outputs = []string{"python_pkg/missing.ts"}
		},
		"stale inputs": func(repo *Repository, _ *[]string) {
			repo.Config.Generation.Producers[0].Inputs = []string{"frontend/missing.py"}
		},
		"handwritten output": func(repo *Repository, _ *[]string) {
			repo.Config.Generation.Producers[0].Outputs = []string{"frontend/schema.py"}
		},
		"input overlap": func(repo *Repository, _ *[]string) {
			repo.Config.Generation.Producers[0].Inputs = []string{"frontend/schema.py", "frontend/*.py"}
		},
		"self consumption": func(repo *Repository, _ *[]string) {
			repo.Config.Generation.Producers[0].Inputs = []string{"python_pkg/client.generated.ts"}
		},
		"duplicate owner": func(repo *Repository, _ *[]string) {
			duplicate := repo.Config.Generation.Producers[0]
			duplicate.Name = "another"
			repo.Config.Generation.Producers = append(repo.Config.Generation.Producers, duplicate)
		},
		"excluded input": func(repo *Repository, _ *[]string) { repo.Config.Scope.Exclude = []string{"frontend/schema.py"} },
		"escaping pattern": func(repo *Repository, _ *[]string) {
			repo.Config.Generation.Producers[0].Inputs = []string{"../source.py"}
		},
		"missing physical file": func(repo *Repository, _ *[]string) {
			if err := os.Remove(filepath.Join(repo.Root, "frontend/schema.py")); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			repo, files := generationRepository(t)
			change(&repo, &files)
			inventory := repo.InspectGeneration(files)
			if len(inventory.Findings) == 0 {
				t.Fatal("invalid producer ownership passed")
			}
			if _, found := inventory.ProducerFor("python_pkg/client.generated.ts"); found {
				t.Fatal("invalid ownership provided a usable producer")
			}
			exceptions := make([]policy.Exception, 0, len(inventory.Findings))
			for _, finding := range inventory.Findings {
				exceptions = append(exceptions, policy.Exception{ID: "attempt", Check: finding.Check, Path: finding.Path, Subject: finding.Subject, Expires: policy.Date{Time: time.Now().Add(time.Hour * 24)}})
			}
			if kept, suppressed := policy.ApplyExceptions(inventory.Findings, exceptions, time.Now()); len(kept) != len(inventory.Findings) || len(suppressed) != 0 {
				t.Fatal("generation ownership failure was suppressible")
			}
		})
	}
}

func TestGenerationInventoryRejectsSymbolicLinksAndDirectories(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"file-link", "parent-link", "directory"} {
		t.Run(kind, func(t *testing.T) {
			repo, files := generationRepository(t)
			target := filepath.Join(repo.Root, "frontend/schema.py")
			if err := os.Remove(target); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "file-link":
				outside := filepath.Join(t.TempDir(), "schema.py")
				if err := os.WriteFile(outside, []byte("source = 1\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, target); err != nil {
					t.Fatal(err)
				}
			case "parent-link":
				directory := filepath.Dir(target)
				if err := os.Remove(directory); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(t.TempDir(), directory); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err := os.Mkdir(target, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			if inventory := repo.InspectGeneration(files); len(inventory.Findings) == 0 {
				t.Fatal("non-regular producer input passed")
			}
		})
	}
}

func TestGenerationInventoryAllowsProducerChainsAndRejectsCycles(t *testing.T) {
	t.Parallel()
	repo, files := generationRepository(t)
	second := repo.Config.Generation.Producers[0]
	second.Name = "python"
	second.Inputs = []string{"python_pkg/client.generated.ts"}
	second.Outputs = []string{"python_pkg/client.generated.py"}
	generationFile(t, repo.Root, second.Outputs[0], "value = 1\n")
	files = append(files, second.Outputs[0])
	repo.Config.Generation.Producers = append(repo.Config.Generation.Producers, second)
	if inventory := repo.InspectGeneration(files); len(inventory.Findings) != 0 {
		t.Fatalf("acyclic producer chain failed: %+v", inventory.Findings)
	}
	repo.Config.Generation.Producers[0].Inputs = slices.Clone(second.Outputs)
	inventory := repo.InspectGeneration(files)
	if len(inventory.Findings) != 1 || inventory.Findings[0].Subject != "producer-cycle" || !strings.Contains(inventory.Findings[0].Message, "client -> python -> client") {
		t.Fatalf("producer cycle = %+v", inventory.Findings)
	}
}

func generationRepository(t *testing.T) (Repository, []string) {
	t.Helper()
	root := t.TempDir()
	generationFile(t, root, "frontend/schema.py", "value = 1\n")
	generationFile(t, root, "python_pkg/client.generated.ts", "export const value=1\n")
	generationFile(t, root, "scripts/generate.sh", "touch executed\n")
	command := policy.GenerationCommand{Argv: []string{"sh", "scripts/generate.sh"}, Cwd: ".", TimeoutSeconds: 900}
	repo := Repository{Root: root, Config: policy.Config{Generation: policy.Generation{Producers: []policy.GenerationProducer{
		{Name: "client", Inputs: []string{"frontend/schema.py"}, Outputs: []string{"python_pkg/client.generated.ts"}, Generate: command, Verify: command},
	}}}}
	return repo, []string{"frontend/schema.py", "python_pkg/client.generated.ts", "scripts/generate.sh"}
}

func generationFile(t *testing.T, root, path, data string) {
	t.Helper()
	absolute := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(absolute), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absolute, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}
