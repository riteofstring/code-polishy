package policy

import (
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

const MaximumGenerationProducers = 64
const MaximumGenerationPatterns = 64

var generationEnvironmentName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type Generation struct {
	Producers []GenerationProducer `json:"producers,omitempty"`
}

type GenerationProducer struct {
	Name     string            `json:"name"`
	Outputs  []string          `json:"outputs"`
	Inputs   []string          `json:"inputs"`
	Generate GenerationCommand `json:"generate"`
	Verify   GenerationCommand `json:"verify"`
}

type GenerationCommand struct {
	Argv           []string `json:"argv"`
	Cwd            string   `json:"cwd,omitempty"`
	Environment    []string `json:"environment,omitempty"`
	TimeoutSeconds int      `json:"timeoutSeconds,omitempty"`
}

func (command GenerationCommand) Clone() GenerationCommand {
	command.Argv = slices.Clone(command.Argv)
	command.Environment = slices.Clone(command.Environment)
	return command
}

func generationDefaults(generation *Generation) {
	for index := range generation.Producers {
		for _, command := range []*GenerationCommand{&generation.Producers[index].Generate, &generation.Producers[index].Verify} {
			defaultString(&command.Cwd, ".")
			defaultInt(&command.TimeoutSeconds, 900)
			slices.Sort(command.Environment)
		}
	}
}

func ValidateGeneration(generation Generation) error {
	if len(generation.Producers) > MaximumGenerationProducers {
		return fmt.Errorf("generation.producers exceeds %d declarations", MaximumGenerationProducers)
	}
	seen := map[string]bool{}
	for _, producer := range generation.Producers {
		if err := validateGenerationProducer(producer, seen); err != nil {
			return err
		}
	}
	return nil
}

func validateGenerationProducer(producer GenerationProducer, seen map[string]bool) error {
	label := "generation producer " + producer.Name
	if err := identifier(producer.Name, label); err != nil {
		return err
	}
	if len(producer.Name) > 128 || seen[producer.Name] {
		return fmt.Errorf("%s has an oversized or duplicate name", label)
	}
	seen[producer.Name] = true
	if err := validateGenerationPatterns(producer.Outputs, label+".outputs"); err != nil {
		return err
	}
	if err := validateGenerationPatterns(producer.Inputs, label+".inputs"); err != nil {
		return err
	}
	if err := validateGenerationCommand(producer.Generate, label+".generate"); err != nil {
		return err
	}
	return validateGenerationCommand(producer.Verify, label+".verify")
}

func validateGenerationPatterns(patterns []string, label string) error {
	if len(patterns) == 0 || len(patterns) > MaximumGenerationPatterns {
		return fmt.Errorf("%s requires 1..%d patterns", label, MaximumGenerationPatterns)
	}
	seen := map[string]bool{}
	for _, pattern := range patterns {
		if !generationPathText(pattern) || pattern == "." || seen[pattern] {
			return fmt.Errorf("%s has a duplicate or noncanonical contained pattern", label)
		}
		if index := strings.IndexAny(pattern, "*?["); index == 0 {
			return fmt.Errorf("%s patterns require a literal repository prefix", label)
		}
		if strings.ContainsAny(pattern, "[]") {
			return fmt.Errorf("%s supports only literal paths, '*' and '?' patterns", label)
		}
		seen[pattern] = true
	}
	return nil
}

func generationPathText(value string) bool {
	return value != "" && len(value) <= 4096 && utf8.ValidString(value) && path.Clean(value) == value &&
		!path.IsAbs(value) && value != ".." && !strings.HasPrefix(value, "../") &&
		!strings.ContainsAny(value, "\\:{}\x00") && strings.IndexFunc(value, unicode.IsControl) < 0
}

func validateGenerationCommand(command GenerationCommand, label string) error {
	if err := validateGenerationArgv(command.Argv, label); err != nil {
		return err
	}
	if !generationPathText(command.Cwd) || strings.ContainsAny(command.Cwd, "*?[") {
		return fmt.Errorf("%s.cwd must be a canonical contained directory", label)
	}
	if command.TimeoutSeconds < 1 || command.TimeoutSeconds > 3600 {
		return fmt.Errorf("%s.timeoutSeconds must be in 1..3600", label)
	}
	return validateGenerationEnvironment(command.Environment, label)
}

func validateGenerationArgv(argv []string, label string) error {
	if len(argv) == 0 || len(argv) > 128 {
		return fmt.Errorf("%s.argv requires 1..128 arguments", label)
	}
	for _, argument := range argv {
		if argument == "" || len(argument) > 4096 || !utf8.ValidString(argument) || strings.IndexFunc(argument, unicode.IsControl) >= 0 {
			return fmt.Errorf("%s.argv contains an empty, oversized, or invalid argument", label)
		}
	}
	if strings.Contains(argv[0], ":") {
		return fmt.Errorf("%s.argv[0] must be a contained portable executable path or command name", label)
	}
	return validateCommandArgv(argv, label)
}

func validateGenerationEnvironment(environment []string, label string) error {
	if len(environment) > 64 {
		return fmt.Errorf("%s.environment exceeds 64 names", label)
	}
	seen := map[string]bool{}
	for _, name := range environment {
		if len(name) > 4096 || !generationEnvironmentName.MatchString(name) || seen[name] {
			return fmt.Errorf("%s.environment requires distinct bounded environment variable names", label)
		}
		seen[name] = true
	}
	return nil
}
