package quality

import (
	"fmt"
	"os"
	"path"
	"regexp"
	"strings"

	"github.com/riteofstring/code-polishy/internal/repository"
	"go.yaml.in/yaml/v4"
	"golang.org/x/text/unicode/norm"
)

type markdownRenderer struct {
	mkdocs     bool
	attributes bool
}

func markdownRendererFor(repo repository.Repository, document string) (markdownRenderer, error) {
	for directory := path.Dir(document); ; directory = path.Dir(directory) {
		for _, name := range []string{"mkdocs.yml", "mkdocs.yaml"} {
			configPath := path.Join(directory, name)
			if _, err := os.Lstat(path.Join(repo.Root, configPath)); os.IsNotExist(err) {
				continue
			}
			data, err := repo.ReadContainedFile(configPath, int(documentationMaximumBytes))
			if err != nil {
				return markdownRenderer{}, err
			}
			var config struct {
				DocsDir    string `yaml:"docs_dir"`
				Extensions any    `yaml:"markdown_extensions"`
			}
			if err := yaml.Unmarshal(data, &config); err != nil {
				return markdownRenderer{}, fmt.Errorf("read %s: %w", configPath, err)
			}
			if config.DocsDir == "" {
				config.DocsDir = "docs"
			}
			scope := path.Clean(path.Join(directory, config.DocsDir))
			if scope == "." || strings.HasPrefix(document, scope+"/") {
				return markdownRenderer{mkdocs: true, attributes: markdownAttributesEnabled(config.Extensions)}, nil
			}
		}
		if directory == "." {
			return markdownRenderer{}, nil
		}
	}
}

func markdownAttributesEnabled(extensions any) bool {
	switch value := extensions.(type) {
	case string:
		name := strings.TrimPrefix(value, "markdown.extensions.")
		return name == "attr_list" || name == "extra"
	case []any:
		for _, extension := range value {
			if markdownAttributesEnabled(extension) {
				return true
			}
		}
	case map[string]any:
		for name := range value {
			if markdownAttributesEnabled(name) {
				return true
			}
		}
	}
	return false
}

var markdownSlugCharacters = regexp.MustCompile(`[^\w\s-]`)
var markdownSlugSeparators = regexp.MustCompile(`[-\s]+`)
var markdownDuplicateSuffix = regexp.MustCompile(`^(.*)_([0-9]+)$`)

func (renderer markdownRenderer) anchor(content string) string {
	if !renderer.mkdocs {
		return markdownAnchor(content)
	}
	content = norm.NFKD.String(content)
	content = strings.ToLower(markdownSlugCharacters.ReplaceAllString(content, ""))
	return markdownSlugSeparators.ReplaceAllString(strings.TrimSpace(content), "-")
}

func markdownConfigurationPath(file string) bool {
	name := path.Base(file)
	return name == "mkdocs.yml" || name == "mkdocs.yaml"
}
