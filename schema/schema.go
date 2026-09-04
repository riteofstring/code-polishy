package schema

import _ "embed"

//go:embed code-polishy.schema.json
var CodePolishy []byte

//go:embed code-polishy-report.schema.json
var CodePolishyReport []byte

//go:embed sarif-schema-2.1.0.json
var SARIF210 []byte
