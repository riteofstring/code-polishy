package javascript

import (
	"path"
	"slices"
	"testing"
)

func TestDeadCodeLiteralPathsCannotSelectGlobNeighbors(t *testing.T) {
	for _, directory := range []string{".", "pages[1]"} {
		t.Run(directory, func(t *testing.T) {
			bundle, root := astroFixture(t)
			files := map[string]string{
				"entry[1].js": "import { used } from './value[1].js'; console.log(used);\n",
				"entry1.js":   "console.log('unreachable neighbor');\n",
				"value[1].js": "export const used = 1;\n",
			}
			project := []string{}
			for name, source := range files {
				file := path.Join(directory, name)
				writeAstroFixture(t, root, file, source)
				project = append(project, file)
			}
			reported, err := bundle.DeadCode(t.Context(), root, ".", []DeadCodeWorkspace{{
				Root: ".", Entry: []string{path.Join(directory, "entry[1].js")}, Project: project,
			}})
			if err != nil {
				t.Fatal(err)
			}
			if len(reported.Unsupported) != 0 || len(reported.Covered) != len(project) ||
				!slices.Equal(reported.UnusedFiles, []string{path.Join(directory, "entry1.js")}) || len(reported.UnusedExports) != 0 {
				t.Fatalf("literal file selection = %+v", reported)
			}
		})
	}
}

func TestDeadCodeUnsupportedWorkspaceCannotClaimCoverage(t *testing.T) {
	bundle, root := astroFixture(t)
	writeAstroFixture(t, root, "workspace[1]/package.json", "{\"name\":\"literal-workspace-fixture\",\"type\":\"module\"}")
	writeAstroFixture(t, root, "workspace[1]/entry.js", "console.log('entry');\n")
	reported, err := bundle.DeadCode(t.Context(), root, ".", []DeadCodeWorkspace{{
		Root: "workspace[1]", Entry: []string{"workspace[1]/entry.js"}, Project: []string{"workspace[1]/entry.js"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(reported.Covered) != 0 || len(reported.Unsupported) != 1 || reported.Unsupported[0].Path != "workspace[1]/entry.js" {
		t.Fatalf("unsupported workspace claimed coverage: %+v", reported)
	}
}
