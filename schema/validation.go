package schema

import (
	"bytes"
	"fmt"
	"sync"
	"time"

	"github.com/dlclark/regexp2"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

const ConfigurationBase = "https://raw.githubusercontent.com/riteofstring/code-polishy/main/schema/"

type Validator struct {
	url      string
	once     sync.Once
	compiled *jsonschema.Schema
	err      error
}

func NewValidator(url string) *Validator {
	return &Validator{url: url}
}

func (validator *Validator) Validate(data []byte) error {
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return err
	}
	validator.once.Do(func() {
		compiler := jsonschema.NewCompiler()
		compiler.UseLoader(shippedLoader{})
		compiler.UseRegexpEngine(compileECMARegexp)
		validator.compiled, validator.err = compiler.Compile(validator.url)
	})
	if validator.err != nil {
		return fmt.Errorf("compile shipped schema: %w", validator.err)
	}
	return validator.compiled.Validate(document)
}

type shippedLoader struct{}

func (shippedLoader) Load(url string) (any, error) {
	resources := map[string][]byte{
		ConfigurationBase + "code-polishy-review-snapshot.schema.json":     CodePolishyReviewSnapshot,
		ConfigurationBase + "code-polishy-architecture-review.schema.json": CodePolishyArchitectureReview,
		ConfigurationBase + "code-polishy.schema.json":                     CodePolishy,
		ConfigurationBase + "code-polishy-supply-chain.schema.json":        CodePolishySupplyChain,
		ConfigurationBase + "code-polishy-python.schema.json":              CodePolishyPython,
		ConfigurationBase + "code-polishy-git-evidence.schema.json":        CodePolishyGitEvidence,
		"https://code-polishy.dev/schema/code-polishy-report.schema.json":  CodePolishyReport,
		"https://json.schemastore.org/sarif-2.1.0.json":                    SARIF210,
	}
	data, exists := resources[url]
	if !exists {
		return nil, fmt.Errorf("schema resource is not shipped: %s", url)
	}
	return jsonschema.UnmarshalJSON(bytes.NewReader(data))
}

type ecmaRegexp regexp2.Regexp

func (expression *ecmaRegexp) MatchString(value string) bool {
	matched, err := (*regexp2.Regexp)(expression).MatchString(value)
	return err == nil && matched
}

func (expression *ecmaRegexp) String() string {
	return (*regexp2.Regexp)(expression).String()
}

func compileECMARegexp(value string) (jsonschema.Regexp, error) {
	expression, err := regexp2.Compile(value, regexp2.ECMAScript)
	if err != nil {
		return nil, err
	}
	expression.MatchTimeout = 100 * time.Millisecond
	return (*ecmaRegexp)(expression), nil
}
