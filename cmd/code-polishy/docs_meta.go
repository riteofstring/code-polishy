package main

import (
	"fmt"
	"os"
	"strings"

	versioneddocs "github.com/riteofstring/code-polishy/internal/documentation"
)

func handleDocsMeta(invocation invocation) int {
	action, arguments, err := parseDocsArguments(invocation.arguments)
	if err != nil {
		return commandUsageError("docs", err.Error())
	}
	library, err := versioneddocs.Open(invocation.policyRoot)
	if err != nil {
		return operationalError(err)
	}
	switch action {
	case "list":
		return printDocumentationTopics(library)
	case "find":
		return findDocumentation(library, arguments)
	case "read":
		return readDocumentation(library, arguments)
	default:
		return commandUsageError("docs", "unknown docs action "+action)
	}
}

func parseDocsArguments(arguments []string) (string, []string, error) {
	if len(arguments) == 0 {
		return "", nil, fmt.Errorf("docs requires list, find, or read")
	}
	action, values := arguments[0], arguments[1:]
	var err error
	switch action {
	case "list":
		err = validateDocsListArguments(values)
	case "find":
		err = validateDocsFindArguments(values)
	case "read":
		err = validateDocsReadArguments(values)
	default:
		return "", nil, fmt.Errorf("unknown docs action %q", action)
	}
	if err != nil {
		return "", nil, err
	}
	return action, values, nil
}

func validateDocsListArguments(arguments []string) error {
	if len(arguments) != 0 {
		return fmt.Errorf("docs list accepts no arguments")
	}
	return nil
}

func validateDocsFindArguments(arguments []string) error {
	if len(arguments) == 0 {
		return fmt.Errorf("docs find requires at least one query term")
	}
	for _, value := range arguments {
		if strings.TrimSpace(value) == "" || strings.HasPrefix(value, "-") {
			return fmt.Errorf("docs find requires non-empty query terms and accepts no options")
		}
	}
	return nil
}

func validateDocsReadArguments(arguments []string) error {
	_, _, err := docsReadOptions(arguments)
	return err
}

func docsReadOptions(arguments []string) ([]string, string, error) {
	topics, section := []string{}, ""
	for len(arguments) > 0 {
		value := arguments[0]
		if value == "--section" || strings.HasPrefix(value, "--section=") {
			heading, consumed, _, err := namedOptionValue(arguments, "--section")
			if err != nil || section != "" || strings.TrimSpace(heading) == "" {
				return nil, "", fmt.Errorf("docs read accepts one --section HEADING for one topic")
			}
			section, arguments = heading, arguments[consumed:]
			continue
		}
		if strings.TrimSpace(value) == "" || strings.HasPrefix(value, "-") {
			return nil, "", fmt.Errorf("docs read requires topic identifiers or exact catalogued paths; unknown option %q", value)
		}
		topics, arguments = append(topics, value), arguments[1:]
	}
	if err := validateDocsReadScope(topics, section); err != nil {
		return nil, "", err
	}
	return topics, section, nil
}

func validateDocsReadScope(topics []string, section string) error {
	if len(topics) == 0 || section != "" && len(topics) != 1 {
		return fmt.Errorf("use ./code-polishyw docs read TOPIC... or ./code-polishyw docs read TOPIC --section HEADING")
	}
	return nil
}

func printDocumentationTopics(library *versioneddocs.Library) int {
	for _, topic := range library.List() {
		if _, err := fmt.Fprintf(os.Stdout, "%s\t%s\n", topic.ID, topic.Title); err != nil {
			return operationalError(fmt.Errorf("write documentation topics: %w", err))
		}
	}
	return 0
}

func findDocumentation(library *versioneddocs.Library, terms []string) int {
	query := strings.Join(terms, " ")
	results, err := library.Find(query)
	if err != nil {
		return documentationError(err)
	}
	if len(results) == 0 {
		fmt.Fprintf(os.Stdout, "No documentation topics matched %q.\n", query)
		return 0
	}
	for _, result := range results {
		if _, err := fmt.Fprintf(os.Stdout, "%s\t%s\t%s\n", result.Topic.ID, result.Topic.Title, result.Excerpt); err != nil {
			return operationalError(fmt.Errorf("write documentation search results: %w", err))
		}
	}
	return 0
}

func readDocumentation(library *versioneddocs.Library, arguments []string) int {
	topics, section, err := docsReadOptions(arguments)
	if err != nil {
		return commandUsageError("docs", err.Error())
	}
	documents := []versioneddocs.Document{}
	seen := map[string]bool{}
	for _, topic := range topics {
		document, err := library.Read(topic)
		if err != nil {
			return documentationError(err)
		}
		if section != "" {
			document, err = document.Section(section)
			if err != nil {
				return documentationError(err)
			}
		}
		if !seen[document.Topic.ID] {
			documents = append(documents, document)
			seen[document.Topic.ID] = true
		}
	}
	if _, err := fmt.Fprint(os.Stdout, renderDocumentation(documents, section)); err != nil {
		return operationalError(fmt.Errorf("write documentation: %w", err))
	}
	return 0
}

func renderDocumentation(documents []versioneddocs.Document, section string) string {
	var output strings.Builder
	for _, document := range documents {
		if len(documents) > 1 || section != "" {
			fmt.Fprintf(&output, "DOCUMENT: %s (%s)\n", document.Topic.ID, document.Topic.Path)
		}
		output.Write(document.Content)
		if len(document.Content) == 0 || document.Content[len(document.Content)-1] != '\n' {
			output.WriteByte('\n')
		}
	}
	return output.String()
}

func documentationError(err error) int {
	if versioneddocs.IsKind(err, versioneddocs.ErrorUnknown) ||
		versioneddocs.IsKind(err, versioneddocs.ErrorAmbiguous) ||
		versioneddocs.IsKind(err, versioneddocs.ErrorInvalidQuery) {
		return commandUsageError("docs", err.Error())
	}
	return operationalError(err)
}
