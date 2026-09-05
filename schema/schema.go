package schema

import _ "embed"

//go:embed code-polishy.schema.json
var CodePolishy []byte

//go:embed code-polishy-supply-chain.schema.json
var CodePolishySupplyChain []byte

//go:embed code-polishy-python.schema.json
var CodePolishyPython []byte

//go:embed code-polishy-report.schema.json
var CodePolishyReport []byte

//go:embed sarif-schema-2.1.0.json
var SARIF210 []byte

//go:embed code-polishy-git-evidence.schema.json
var CodePolishyGitEvidence []byte

//go:embed code-polishy-architecture-review.schema.json
var CodePolishyArchitectureReview []byte

//go:embed code-polishy-review-snapshot.schema.json
var CodePolishyReviewSnapshot []byte
