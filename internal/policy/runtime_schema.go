package policy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/dlclark/regexp2"
	policyschema "github.com/riteofstring/code-polishy/schema"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

const configurationSchemaBase = "https://raw.githubusercontent.com/riteofstring/code-polishy/main/schema/"
const runtimeSchemaURL = configurationSchemaBase + "code-polishy.schema.json"

var runtimeSchemaOnce sync.Once
var runtimeSchema *jsonschema.Schema
var runtimeSchemaError error

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

func validateRuntimeSchema(data []byte) error {
	var document any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&document); err != nil {
		return err
	}
	if err := requireEOF(decoder); err != nil {
		return err
	}
	runtimeSchemaOnce.Do(func() {
		compiler := jsonschema.NewCompiler()
		compiler.UseRegexpEngine(compileECMARegexp)
		if err := addRuntimeSchemaResources(compiler); err != nil {
			runtimeSchemaError = err
			return
		}
		runtimeSchema, runtimeSchemaError = compiler.Compile(runtimeSchemaURL)
	})
	if runtimeSchemaError != nil {
		return fmt.Errorf("compile shipped configuration schema: %w", runtimeSchemaError)
	}
	if err := runtimeSchema.Validate(document); err != nil {
		return fmt.Errorf("configuration does not match shipped schema: %w", err)
	}
	return nil
}

func addRuntimeSchemaResources(compiler *jsonschema.Compiler) error {
	for _, resource := range []struct {
		url  string
		data []byte
	}{
		{runtimeSchemaURL, policyschema.CodePolishy},
		{configurationSchemaBase + "code-polishy-supply-chain.schema.json", policyschema.CodePolishySupplyChain},
		{configurationSchemaBase + "code-polishy-python.schema.json", policyschema.CodePolishyPython},
	} {
		document, err := jsonschema.UnmarshalJSON(bytes.NewReader(resource.data))
		if err != nil {
			return err
		}
		if err := compiler.AddResource(resource.url, document); err != nil {
			return err
		}
	}
	return nil
}
