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
		return readDocumentation(library, arguments[0])
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
	if len(arguments) != 1 || strings.TrimSpace(arguments[0]) == "" || strings.HasPrefix(arguments[0], "-") {
		return fmt.Errorf("docs read requires exactly one topic identifier")
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

func readDocumentation(library *versioneddocs.Library, topic string) int {
	document, err := library.Read(topic)
	if err != nil {
		return documentationError(err)
	}
	if _, err := os.Stdout.Write(document.Content); err != nil {
		return operationalError(fmt.Errorf("write documentation topic: %w", err))
	}
	if len(document.Content) == 0 || document.Content[len(document.Content)-1] != '\n' {
		if _, err := fmt.Fprintln(os.Stdout); err != nil {
			return operationalError(fmt.Errorf("finish documentation topic: %w", err))
		}
	}
	return 0
}

func documentationError(err error) int {
	if versioneddocs.IsKind(err, versioneddocs.ErrorUnknown) ||
		versioneddocs.IsKind(err, versioneddocs.ErrorAmbiguous) ||
		versioneddocs.IsKind(err, versioneddocs.ErrorInvalidQuery) {
		return commandUsageError("docs", err.Error())
	}
	return operationalError(err)
}
