package versioneddocs

import (
	"strings"
	"testing"
)

func TestDocumentSectionPreservesSubsectionsAndIgnoresFencedHeadings(t *testing.T) {
	document := Document{Topic: Topic{ID: "guide"}, Content: []byte("# Guide\n## Start\nText.\n````md\n## Ignored\n```\n````\n### Detail\nMore.\n## End\nDone.\n")}
	section, err := document.Section("Start")
	want := "## Start\nText.\n````md\n## Ignored\n```\n````\n### Detail\nMore.\n"
	if err != nil || string(section.Content) != want {
		t.Fatalf("section = %q, %v", section.Content, err)
	}
	if _, err := document.Section("Ignored"); !IsKind(err, ErrorInvalidQuery) || !strings.Contains(err.Error(), "./code-polishyw docs read guide --section 'Guide > Start'") {
		t.Fatalf("missing heading recovery = %v", err)
	}
}

func TestDocumentSectionDisambiguatesHeadingAncestry(t *testing.T) {
	document := Document{Topic: Topic{ID: "guide"}, Content: []byte("# Guide\n## Start\n### Steps\nFirst.\n## Finish\n### Steps ###\nLast.\n")}
	if _, err := document.Section("Steps"); !IsKind(err, ErrorInvalidQuery) || !strings.Contains(err.Error(), "ambiguous") || !strings.Contains(err.Error(), "Guide > Finish > Steps") {
		t.Fatalf("ambiguous heading = %v", err)
	}
	section, err := document.Section("Guide > Finish > Steps")
	if err != nil || string(section.Content) != "### Steps ###\nLast.\n" {
		t.Fatalf("qualified section = %q, %v", section.Content, err)
	}
}
