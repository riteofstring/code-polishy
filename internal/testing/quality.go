package testing

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

// SourceFindings performs conservative, cheap checks for test bodies that
// cannot provide evidence. Semantic weakness is left to supplemental mutation
// suites because source-shape heuristics cannot prove test effectiveness.
func SourceFindings(repo repository.Repository, files []string) []policy.Finding {
	findings := []policy.Finding{}
	for _, path := range files {
		if repo.Language(path) != "go" || repo.IsGenerated(path) || !strings.HasSuffix(strings.ToLower(path), "_test.go") {
			continue
		}
		findings = append(findings, goTestSourceFindings(repo, path)...)
	}
	return findings
}

func goTestSourceFindings(repo repository.Repository, path string) []policy.Finding {
	data, err := repo.Read(path)
	if err != nil {
		return nil
	}
	set := token.NewFileSet()
	file, err := parser.ParseFile(set, path, data, parser.SkipObjectResolution)
	if err != nil {
		return nil
	}
	findings := []policy.Finding{}
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || !isGoTestFunction(function) {
			continue
		}
		parameter := testParameterName(function)
		switch {
		case len(function.Body.List) == 0:
			findings = append(findings, testEvidenceFinding(path, function.Name.Name, "test body is empty"))
		case onlyHelpersAndSkip(function.Body.List, parameter):
			findings = append(findings, testEvidenceFinding(path, function.Name.Name, "test unconditionally skips and provides no behavior evidence"))
		}
		findings = append(findings, emptySubtestFindings(set, path, function, parameter)...)
	}
	return findings
}

func isGoTestFunction(function *ast.FuncDecl) bool {
	name := function.Name.Name
	testEntry := isGoTestEntry(name, "Test") || isGoTestEntry(name, "Benchmark") || isGoTestEntry(name, "Fuzz")
	return function.Recv == nil && function.Body != nil && testEntry &&
		function.Type.Params != nil && len(function.Type.Params.List) == 1
}

func isGoTestEntry(name, prefix string) bool {
	if !strings.HasPrefix(name, prefix) {
		return false
	}
	remainder := strings.TrimPrefix(name, prefix)
	first, _ := utf8.DecodeRuneInString(remainder)
	return !unicode.IsLower(first)
}

func testParameterName(function *ast.FuncDecl) string {
	parameter := function.Type.Params.List[0]
	if len(parameter.Names) != 1 {
		return ""
	}
	return parameter.Names[0].Name
}

func onlyHelpersAndSkip(statements []ast.Stmt, parameter string) bool {
	if parameter == "" {
		return false
	}
	sawSkip := false
	for _, statement := range statements {
		expression, ok := statement.(*ast.ExprStmt)
		if !ok {
			return false
		}
		call, ok := expression.X.(*ast.CallExpr)
		if !ok {
			return false
		}
		method := receiverMethod(call.Fun, parameter)
		switch method {
		case "Helper":
			continue
		case "Skip", "Skipf", "SkipNow":
			sawSkip = true
		default:
			return false
		}
	}
	return sawSkip
}

func emptySubtestFindings(set *token.FileSet, path string, function *ast.FuncDecl, parameter string) []policy.Finding {
	findings := []policy.Finding{}
	ast.Inspect(function.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || receiverMethod(call.Fun, parameter) != "Run" || len(call.Args) != 2 {
			return true
		}
		callback, ok := call.Args[1].(*ast.FuncLit)
		if !ok || callback.Body == nil || len(callback.Body.List) != 0 {
			return true
		}
		position := set.Position(callback.Pos())
		subject := function.Name.Name + ":line-" + strconv.Itoa(position.Line)
		findings = append(findings, testEvidenceFinding(path, subject, "subtest body is empty"))
		return true
	})
	return findings
}

func receiverMethod(expression ast.Expr, receiver string) string {
	selector, ok := expression.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	identifier, ok := selector.X.(*ast.Ident)
	if !ok || identifier.Name != receiver {
		return ""
	}
	return selector.Sel.Name
}

func testEvidenceFinding(path, subject, message string) policy.Finding {
	return policy.Finding{Check: "quality.testEvidence", Path: path, Subject: subject, Message: message}
}
