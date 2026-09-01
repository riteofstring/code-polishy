package versioneddocs

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestOpenListsPublicTopicsInStableOrderAndReadsAliases(t *testing.T) {
	root := documentationFixture(t, []Topic{
		fixtureTopic("zebra", "docs/zebra.md", "Zebra", false),
		{
			ID: "alpha", Path: "docs/alpha.md", Title: "Alpha", Summary: "Alpha summary.",
			Aliases: []string{"first"}, Public: true,
		},
	}, map[string][]byte{
		"docs/alpha.md": []byte("# Alpha\n\nAlpha body.\n"),
		"docs/zebra.md": []byte("# Zebra\n\nZebra body.\n"),
	})
	library, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	topics := library.List()
	if len(topics) != 1 || topics[0].ID != "alpha" {
		t.Fatalf("topics = %+v", topics)
	}
	topics[0].Aliases[0] = "changed"
	if library.List()[0].Aliases[0] != "first" {
		t.Fatal("List returned mutable catalog state")
	}
	document, err := library.Read("FIRST")
	if err != nil {
		t.Fatal(err)
	}
	if document.Topic.ID != "alpha" || string(document.Content) != "# Alpha\n\nAlpha body.\n" {
		t.Fatalf("document = %+v", document)
	}
	document.Content[0] = 'x'
	again, err := library.Read("alpha")
	if err != nil || string(again.Content) != "# Alpha\n\nAlpha body.\n" {
		t.Fatalf("second read = %q, %v", again.Content, err)
	}
}

func TestOpenRejectsInvalidCatalogContracts(t *testing.T) {
	valid := fixtureTopic("alpha", "docs/alpha.md", "Alpha", true)
	tests := []struct {
		name   string
		mutate func([]Topic) []Topic
	}{
		{name: "no topics", mutate: func([]Topic) []Topic { return nil }},
		{name: "invalid identifier", mutate: mutateTopic(func(topic *Topic) { topic.ID = "Alpha" })},
		{name: "escaping path", mutate: mutateTopic(func(topic *Topic) { topic.Path = "../alpha.md" })},
		{name: "plan path", mutate: mutateTopic(func(topic *Topic) { topic.Path = "docs/plans/alpha.md" })},
		{name: "non-markdown path", mutate: mutateTopic(func(topic *Topic) { topic.Path = "docs/alpha.txt" })},
		{name: "multiline title", mutate: mutateTopic(func(topic *Topic) { topic.Title = "Alpha\nTitle" })},
		{name: "empty summary", mutate: mutateTopic(func(topic *Topic) { topic.Summary = "" })},
		{name: "unsorted aliases", mutate: mutateTopic(func(topic *Topic) { topic.Aliases = []string{"zulu", "beta"} })},
		{name: "invalid alias", mutate: mutateTopic(func(topic *Topic) { topic.Aliases = []string{"bad alias"} })},
		{name: "duplicate identifier", mutate: func(topics []Topic) []Topic { return append(topics, topics[0]) }},
		{name: "duplicate path", mutate: func(topics []Topic) []Topic {
			other := fixtureTopic("beta", topics[0].Path, "Beta", true)
			return append(topics, other)
		}},
		{name: "duplicate alias", mutate: func(topics []Topic) []Topic {
			topics[0].Aliases = []string{"shared"}
			other := fixtureTopic("beta", "docs/beta.md", "Beta", true)
			other.Aliases = []string{"shared"}
			return append(topics, other)
		}},
		{name: "identifier alias collision", mutate: func(topics []Topic) []Topic {
			other := fixtureTopic("beta", "docs/beta.md", "Beta", true)
			other.Aliases = []string{"alpha"}
			return append(topics, other)
		}},
		{name: "no public topics", mutate: mutateTopic(func(topic *Topic) { topic.Public = false })},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			topics := test.mutate([]Topic{valid})
			files := map[string][]byte{
				"docs/alpha.md":       []byte("# Alpha\n"),
				"docs/beta.md":        []byte("# Beta\n"),
				"docs/plans/alpha.md": []byte("# Alpha\n"),
			}
			root := documentationFixture(t, topics, files)
			if _, err := Open(root); !IsKind(err, ErrorInvalidCatalog) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestOpenRejectsMalformedMissingAndUnsafeFiles(t *testing.T) {
	t.Run("unknown catalog field", func(t *testing.T) {
		root := t.TempDir()
		writeDocumentationFile(t, root, CatalogPath, []byte(`{"version":1,"topics":[],"extra":true}`))
		if _, err := Open(root); !IsKind(err, ErrorInvalidCatalog) {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("extra catalog value", func(t *testing.T) {
		root := t.TempDir()
		writeDocumentationFile(t, root, CatalogPath, []byte(`{"version":1,"topics":[]} {}`))
		if _, err := Open(root); !IsKind(err, ErrorInvalidCatalog) {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("non utf8 catalog", func(t *testing.T) {
		root := t.TempDir()
		writeDocumentationFile(t, root, CatalogPath, []byte{0xff})
		if _, err := Open(root); !IsKind(err, ErrorInvalidCatalog) {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("oversized catalog", func(t *testing.T) {
		root := t.TempDir()
		writeDocumentationFile(t, root, CatalogPath, bytes.Repeat([]byte("x"), MaxCatalogBytes+1))
		if _, err := Open(root); !IsKind(err, ErrorUnavailable) {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("missing catalog", func(t *testing.T) {
		if _, err := Open(t.TempDir()); !IsKind(err, ErrorUnavailable) {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("missing document", func(t *testing.T) {
		root := documentationFixture(t, []Topic{fixtureTopic("alpha", "docs/missing.md", "Alpha", true)}, nil)
		if _, err := Open(root); !IsKind(err, ErrorInvalidCatalog) {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("directory document", func(t *testing.T) {
		root := documentationFixture(t, []Topic{fixtureTopic("alpha", "docs/alpha.md", "Alpha", true)}, nil)
		if err := os.MkdirAll(filepath.Join(root, "docs", "alpha.md"), 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := Open(root); !IsKind(err, ErrorInvalidCatalog) {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("symlinked document", func(t *testing.T) {
		root := documentationFixture(t, []Topic{fixtureTopic("alpha", "docs/alpha.md", "Alpha", true)}, map[string][]byte{
			"outside.md": []byte("# Alpha\n"),
		})
		link := filepath.Join(root, "docs", "alpha.md")
		if err := os.Symlink(filepath.Join(root, "outside.md"), link); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
		if _, err := Open(root); !IsKind(err, ErrorInvalidCatalog) {
			t.Fatalf("error = %v", err)
		}
	})
}

func TestReadRejectsDamagedDocumentsAndSuggestsCloseTopics(t *testing.T) {
	root := documentationFixture(t, []Topic{
		fixtureTopic("behavior-review", "docs/behavior.md", "Behavior Review", true),
		fixtureTopic("testing", "docs/testing.md", "Testing", true),
	}, map[string][]byte{
		"docs/behavior.md": []byte("# Behavior Review\n"),
		"docs/testing.md":  []byte("# Testing\n"),
	})
	library, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := library.Read("behaviour-review"); !IsKind(err, ErrorUnknown) || !strings.Contains(err.Error(), `did you mean "behavior-review"`) {
		t.Fatalf("suggestion error = %v", err)
	}
	if _, err := library.Read("../behavior-review"); !IsKind(err, ErrorUnknown) {
		t.Fatalf("unsafe lookup error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "behavior.md"), []byte{0xff}, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := library.Read("behavior-review"); !IsKind(err, ErrorUnavailable) {
		t.Fatalf("non-UTF8 error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "testing.md"), bytes.Repeat([]byte("x"), MaxDocumentBytes+1), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := library.Read("testing"); !IsKind(err, ErrorUnavailable) {
		t.Fatalf("oversized error = %v", err)
	}
}

func TestResolveReportsAmbiguousCatalogState(t *testing.T) {
	library := &Library{
		topics: []Topic{{ID: "alpha"}, {ID: "beta"}},
		byName: map[string][]int{"shared": {0, 1}},
	}
	if _, err := library.resolve("shared"); !IsKind(err, ErrorAmbiguous) || !strings.Contains(err.Error(), "alpha, beta") {
		t.Fatalf("error = %v", err)
	}
}

func TestFindUsesAllTermsStableRankingAndBoundedExcerpts(t *testing.T) {
	longLine := strings.Repeat("long regression evidence ", 20)
	root := documentationFixture(t, []Topic{
		fixtureTopic("behavior-review", "docs/behavior.md", "Behavior Review", true),
		fixtureTopic("testing", "docs/testing.md", "Testing", true),
		fixtureTopic("workflow", "docs/workflow.md", "Workflow", true),
	}, map[string][]byte{
		"docs/behavior.md": []byte("# Behavior Review\n\n## Regression proofs\n\n" + longLine + "\nRed baseline and green candidate.\n"),
		"docs/testing.md":  []byte("# Testing\n\nBehavior tests run against a candidate.\n"),
		"docs/workflow.md": []byte("# Workflow\n\nBehavior checks happen here. A candidate is reviewed later.\n"),
	})
	library, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	results, err := library.Find("BEHAVIOR REVIEW")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 || results[0].Topic.ID != "behavior-review" {
		t.Fatalf("results = %+v", results)
	}
	results, err = library.Find("behavior candidate")
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, len(results))
	for index, result := range results {
		got[index] = result.Topic.ID
		if utf8.RuneCountInString(result.Excerpt) > MaxExcerptRunes {
			t.Fatalf("excerpt exceeds bound: %q", result.Excerpt)
		}
	}
	if !reflect.DeepEqual(got, []string{"behavior-review", "testing", "workflow"}) {
		t.Fatalf("result order = %v", got)
	}
	if results, err := library.Find("absent term"); err != nil || len(results) != 0 {
		t.Fatalf("no results = %+v, %v", results, err)
	}
	if _, err := library.Find(" \t "); !IsKind(err, ErrorInvalidQuery) {
		t.Fatalf("empty query error = %v", err)
	}
}

func TestFindLimitsResultsByStableTopicOrder(t *testing.T) {
	topics := make([]Topic, 0, MaxSearchResults+5)
	files := map[string][]byte{}
	for index := 0; index < MaxSearchResults+5; index++ {
		id := fmt.Sprintf("topic-%02d", index)
		path := "docs/" + id + ".md"
		topics = append(topics, fixtureTopic(id, path, "Topic", true))
		files[path] = []byte("# Topic\n\nShared needle.\n")
	}
	root := documentationFixture(t, topics, files)
	library, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	results, err := library.Find("needle")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != MaxSearchResults || results[0].Topic.ID != "topic-00" || results[len(results)-1].Topic.ID != "topic-19" {
		t.Fatalf("bounded results = %+v", results)
	}
}

func TestCheckedInCatalogOwnsEveryPermanentDocument(t *testing.T) {
	root := repositoryRoot(t)
	library, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	cataloged := []string{}
	for _, topic := range library.List() {
		cataloged = append(cataloged, topic.Path)
		document, err := library.Read(topic.ID)
		if err != nil {
			t.Fatal(err)
		}
		firstLine, _, _ := strings.Cut(string(document.Content), "\n")
		if firstLine != "# "+topic.Title {
			t.Errorf("%s heading = %q, want %q", topic.Path, firstLine, "# "+topic.Title)
		}
	}
	sort.Strings(cataloged)
	permanent := []string{}
	err = filepath.WalkDir(filepath.Join(root, "docs"), func(filePath string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, filePath)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if entry.IsDir() && relative == "docs/plans" {
			return filepath.SkipDir
		}
		if !entry.IsDir() && strings.EqualFold(filepath.Ext(entry.Name()), ".md") {
			permanent = append(permanent, relative)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(permanent)
	if !slices.Equal(cataloged, permanent) {
		t.Fatalf("cataloged = %v\npermanent = %v", cataloged, permanent)
	}
}

func TestSourceAndCopiedReleaseRootsReadIdenticalDocumentation(t *testing.T) {
	sourceRoot := repositoryRoot(t)
	source, err := Open(sourceRoot)
	if err != nil {
		t.Fatal(err)
	}
	installedRoot := t.TempDir()
	for _, topic := range source.List() {
		document, err := source.Read(topic.ID)
		if err != nil {
			t.Fatal(err)
		}
		writeDocumentationFile(t, installedRoot, topic.Path, document.Content)
	}
	catalogData, err := os.ReadFile(filepath.Join(sourceRoot, filepath.FromSlash(CatalogPath)))
	if err != nil {
		t.Fatal(err)
	}
	writeDocumentationFile(t, installedRoot, CatalogPath, catalogData)
	installed, err := Open(installedRoot)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(source.List(), installed.List()) {
		t.Fatal("source and copied release catalogs differ")
	}
	for _, topic := range source.List() {
		left, leftErr := source.Read(topic.ID)
		right, rightErr := installed.Read(topic.ID)
		if leftErr != nil || rightErr != nil || !bytes.Equal(left.Content, right.Content) {
			t.Fatalf("topic %s differs: %v, %v", topic.ID, leftErr, rightErr)
		}
	}
}

func mutateTopic(mutation func(*Topic)) func([]Topic) []Topic {
	return func(topics []Topic) []Topic {
		mutation(&topics[0])
		return topics
	}
}

func fixtureTopic(id, path, title string, public bool) Topic {
	return Topic{ID: id, Path: path, Title: title, Summary: title + " summary.", Aliases: []string{}, Public: public}
}

func documentationFixture(t *testing.T, topics []Topic, files map[string][]byte) string {
	t.Helper()
	root := t.TempDir()
	for path, data := range files {
		writeDocumentationFile(t, root, path, data)
	}
	data, err := json.Marshal(catalog{Version: CatalogVersion, Topics: topics})
	if err != nil {
		t.Fatal(err)
	}
	writeDocumentationFile(t, root, CatalogPath, append(data, '\n'))
	return root
}

func writeDocumentationFile(t *testing.T, root, relative string, data []byte) {
	t.Helper()
	filePath := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filePath, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal(errors.New("resolve test source path"))
	}
	return filepath.Clean(filepath.Join(filepath.Dir(source), "..", ".."))
}
