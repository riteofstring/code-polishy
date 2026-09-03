package quality

import (
	"fmt"
	"go/ast"
	"go/build/constraint"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

type sourceText struct {
	lineStarts []int
}

type sourceAnnotation struct {
	offset  int
	text    string
	allowed bool
}

type sourceCommentScan struct {
	annotations []sourceAnnotation
	err         error
}

func newSourceText(data []byte) sourceText {
	starts := []int{0}
	for index, value := range data {
		if value == '\n' {
			starts = append(starts, index+1)
		}
	}
	return sourceText{lineStarts: starts}
}

func (source sourceText) location(offset int) (int, int) {
	index := sort.Search(len(source.lineStarts), func(index int) bool {
		return source.lineStarts[index] > offset
	}) - 1
	if index < 0 {
		index = 0
	}
	return index + 1, offset - source.lineStarts[index] + 1
}

func sourceCommentFindings(repo repository.Repository, files []string) []policy.Finding {
	if repo.Config.Quality.CommentsAllowed() {
		return nil
	}
	return strictSourceCommentFindings(repo, files)
}

func strictSourceCommentFindings(repo repository.Repository, files []string) []policy.Finding {
	findings := []policy.Finding{}
	for _, path := range sourceCommentPaths(files) {
		if repo.IsGenerated(path) {
			continue
		}
		language := sourceCommentLanguage(repo, path)
		if language == "" || language == "typescript" || !sourceCommentLanguageSupported(language) {
			continue
		}
		data, err := repo.Read(path)
		if err != nil {
			findings = append(findings, sourceCommentCoverageFinding(path, language, "the file could not be read: "+err.Error()))
			continue
		}
		result := scanSourceComments(path, language, data)
		if result.err != nil {
			findings = append(findings, sourceCommentCoverageFinding(path, language, result.err.Error()))
			continue
		}
		source := newSourceText(data)
		for _, annotation := range result.annotations {
			if annotation.allowed || sourceAnnotationAllowed(repo, path, language, data, annotation) {
				continue
			}
			line, column := source.location(annotation.offset)
			findings = append(findings, policy.Finding{
				Check: "policy.sourceComment", Path: path, Subject: fmt.Sprintf("%d:%d", line, column),
				Message: "prose comments and docstrings are forbidden; move durable context to a design document or use an allowed machine directive",
			})
		}
	}
	return findings
}

func sourceCommentCoverageFindings(repo repository.Repository, files []string) []policy.Finding {
	if repo.Config.Quality.CommentsAllowed() {
		return nil
	}
	findings := []policy.Finding{}
	for _, path := range sourceCommentPaths(files) {
		if repo.IsGenerated(path) {
			continue
		}
		language := sourceCommentLanguage(repo, path)
		if language == "" || language == "typescript" || sourceCommentLanguageSupported(language) || !repo.IsExecutableSource(path) {
			continue
		}
		findings = append(findings, sourceCommentCoverageFinding(path, language, "the policy-owned source-comment scanner does not support this executable language"))
	}
	return findings
}

func sourceCommentPaths(files []string) []string {
	paths := append([]string{}, files...)
	sort.Strings(paths)
	unique := make([]string, 0, len(paths))
	for _, path := range paths {
		if len(unique) == 0 || unique[len(unique)-1] != path {
			unique = append(unique, path)
		}
	}
	return unique
}

func sourceCommentLanguage(repo repository.Repository, path string) string {
	language := repo.SourceCommentLanguage(path)
	if language != "typescript" {
		return language
	}
	extension := strings.ToLower(filepath.Ext(path))
	if javascriptSourceExtensions[extension] {
		return "typescript"
	}
	return "typescript-unsupported"
}

func sourceCommentLanguageSupported(language string) bool {
	switch language {
	case "go", "python", "shell", "css", "html", "powershell":
		return true
	}
	return false
}

func scanSourceComments(path, language string, data []byte) sourceCommentScan {
	switch language {
	case "go":
		annotations, err := scanGoSource(path, data)
		return sourceCommentScan{annotations: annotations, err: err}
	case "python":
		annotations, err := scanPythonSource(data)
		return sourceCommentScan{annotations: annotations, err: err}
	case "shell":
		annotations, err := scanShellSource(data)
		return sourceCommentScan{annotations: annotations, err: err}
	case "css":
		annotations, err := scanCSSSource(data)
		return sourceCommentScan{annotations: annotations, err: err}
	case "html":
		annotations, err := scanHTMLSource(data)
		return sourceCommentScan{annotations: annotations, err: err}
	case "powershell":
		annotations, err := scanPowerShellSource(data)
		return sourceCommentScan{annotations: annotations, err: err}
	default:
		return sourceCommentScan{err: fmt.Errorf("the policy-owned source-comment scanner does not support %s source", language)}
	}
}

func sourceCommentCoverageFinding(path, language, reason string) policy.Finding {
	return policy.Finding{
		Check: "policy.sourceCommentCoverage", Path: path, Subject: language,
		Message: "the policy-owned source-comment scanner could not decide this source: " + reason,
	}
}

func scanGoSource(path string, data []byte) ([]sourceAnnotation, error) {
	set := token.NewFileSet()
	parsed, err := parser.ParseFile(set, path, data, parser.ParseComments|parser.AllErrors)
	if err != nil {
		return nil, err
	}
	annotations := []sourceAnnotation{}
	for _, group := range parsed.Comments {
		for _, comment := range group.List {
			annotations = append(annotations, sourceAnnotation{offset: set.Position(comment.Pos()).Offset, text: comment.Text})
		}
	}
	goMarkAllowedAnnotations(path, data, set, parsed, annotations)
	return annotations, nil
}

func sourceAnnotationAllowed(repo repository.Repository, path, language string, data []byte, annotation sourceAnnotation) bool {
	switch language {
	case "go":
		return annotation.allowed
	case "python":
		return byteZeroShebang(data, annotation) || pythonEncodingAllowed(data, annotation)
	case "shell":
		return byteZeroShebang(data, annotation) || shellcheckSourceAllowed(repo, data, annotation) || shellSBATCHAllowed(data, annotation)
	case "powershell":
		return byteZeroShebang(data, annotation)
	}
	return false
}

func byteZeroShebang(data []byte, annotation sourceAnnotation) bool {
	return annotation.offset == 0 && strings.HasPrefix(annotation.text, "#!") && strings.TrimSpace(strings.TrimPrefix(annotation.text, "#!")) != ""
}

func sourceLineStart(data []byte, offset int) bool {
	return offset == 0 || offset > 0 && (data[offset-1] == '\n' || data[offset-1] == '\r')
}

func sourceLineBlank(data []byte) bool {
	for _, value := range data {
		if value != ' ' && value != '\t' && value != '\f' {
			return false
		}
	}
	return true
}

func goMarkAllowedAnnotations(path string, data []byte, set *token.FileSet, parsed *ast.File, annotations []sourceAnnotation) {
	cgoPreamble := goCgoPreambleOffsets(set, parsed)
	packageOffset := set.Position(parsed.Package).Offset
	buildCount := 0
	for _, annotation := range annotations {
		if strings.HasPrefix(annotation.text, "//go:build") {
			buildCount++
		}
	}
	for index := range annotations {
		annotation := &annotations[index]
		if cgoPreamble[annotation.offset] {
			annotation.allowed = true
			continue
		}
		annotation.allowed = goAnnotationAllowed(path, data, set, parsed, packageOffset, buildCount, *annotation)
	}
}

func goCgoPreambleOffsets(set *token.FileSet, parsed *ast.File) map[int]bool {
	offsets := map[int]bool{}
	for _, declaration := range parsed.Decls {
		group, ok := declaration.(*ast.GenDecl)
		if !ok || group.Tok != token.IMPORT {
			continue
		}
		for _, specification := range group.Specs {
			importSpec, ok := specification.(*ast.ImportSpec)
			if !ok || importSpec.Path == nil || importSpec.Path.Value != "\"C\"" {
				continue
			}
			goMarkCommentGroupOffsets(offsets, set, importSpec.Doc)
			if len(group.Specs) == 1 {
				goMarkCommentGroupOffsets(offsets, set, group.Doc)
			}
		}
	}
	return offsets
}

func goMarkCommentGroupOffsets(offsets map[int]bool, set *token.FileSet, group *ast.CommentGroup) {
	if group == nil {
		return
	}
	for _, comment := range group.List {
		offsets[set.Position(comment.Pos()).Offset] = true
	}
}

func goAnnotationAllowed(path string, data []byte, set *token.FileSet, parsed *ast.File, packageOffset, buildCount int, annotation sourceAnnotation) bool {
	if annotation.text == "" || !strings.HasPrefix(annotation.text, "//") {
		return false
	}
	if allowed, known := goStandaloneAnnotationAllowed(path, data, parsed, packageOffset, buildCount, annotation); known {
		return allowed
	}
	declaration, found := goDirectiveDeclaration(set, parsed, annotation.offset)
	return found && goDeclarationAnnotationAllowed(parsed, declaration, annotation)
}

func goStandaloneAnnotationAllowed(path string, data []byte, parsed *ast.File, packageOffset, buildCount int, annotation sourceAnnotation) (bool, bool) {
	if strings.HasPrefix(annotation.text, "//go:build") {
		return goBuildAllowed(data, packageOffset, buildCount, annotation), true
	}
	if strings.HasPrefix(annotation.text, "//go:generate") {
		return goGenerateAllowed(data, annotation), true
	}
	if strings.HasPrefix(annotation.text, "//line") {
		return goLineAllowed(data, annotation), true
	}
	if strings.HasPrefix(annotation.text, "//go:debug") {
		return goDebugAllowed(path, data, parsed, packageOffset, annotation), true
	}
	return false, false
}

func goDeclarationAnnotationAllowed(parsed *ast.File, declaration goDeclaration, annotation sourceAnnotation) bool {
	if strings.HasPrefix(annotation.text, "//go:embed ") {
		return goEmbedAnnotationAllowed(declaration, annotation.text)
	}
	if strings.HasPrefix(annotation.text, "//go:linkname ") {
		return goLinknameAnnotationAllowed(declaration, annotation.text)
	}
	if strings.HasPrefix(annotation.text, "//go:wasmimport ") {
		return goWasmImportAnnotationAllowed(declaration.function, annotation.text)
	}
	if strings.HasPrefix(annotation.text, "//go:wasmexport ") {
		return goWasmExportAnnotationAllowed(declaration.function, annotation.text)
	}
	if strings.HasPrefix(annotation.text, "//export ") {
		return goFileImportsC(parsed) && goExportAllowed(annotation.text, declaration.function)
	}
	return goFunctionAnnotationAllowed(declaration.function, annotation.text)
}

func goEmbedAnnotationAllowed(declaration goDeclaration, text string) bool {
	if !declaration.variable {
		return false
	}
	return goEmbedArgumentsAllowed(strings.TrimPrefix(text, "//go:embed "))
}

func goLinknameAnnotationAllowed(declaration goDeclaration, text string) bool {
	if declaration.function == nil && !declaration.variable {
		return false
	}
	return goDirectiveFieldsAllowed(text, "//go:linkname ", 1, 2)
}

func goWasmImportAnnotationAllowed(declaration *ast.FuncDecl, text string) bool {
	if declaration == nil || declaration.Body != nil {
		return false
	}
	return goDirectiveFieldsAllowed(text, "//go:wasmimport ", 2, 2)
}

func goWasmExportAnnotationAllowed(declaration *ast.FuncDecl, text string) bool {
	if declaration == nil || declaration.Body == nil {
		return false
	}
	return goDirectiveFieldsAllowed(text, "//go:wasmexport ", 1, 1)
}

func goFunctionAnnotationAllowed(declaration *ast.FuncDecl, text string) bool {
	if !goFunctionDirective[text] || declaration == nil {
		return false
	}
	if text == "//go:noescape" {
		return declaration.Body == nil
	}
	return true
}

func goFileImportsC(parsed *ast.File) bool {
	for _, declaration := range parsed.Decls {
		group, ok := declaration.(*ast.GenDecl)
		if !ok || group.Tok != token.IMPORT {
			continue
		}
		for _, specification := range group.Specs {
			importSpec, ok := specification.(*ast.ImportSpec)
			if ok && importSpec.Path != nil && importSpec.Path.Value == "\"C\"" {
				return true
			}
		}
	}
	return false
}

type goDeclaration struct {
	function *ast.FuncDecl
	variable bool
}

func goDirectiveDeclaration(set *token.FileSet, parsed *ast.File, offset int) (goDeclaration, bool) {
	for _, declaration := range parsed.Decls {
		switch value := declaration.(type) {
		case *ast.FuncDecl:
			if goCommentGroupContains(set, value.Doc, offset) {
				return goDeclaration{function: value}, true
			}
		case *ast.GenDecl:
			if value.Tok != token.VAR {
				continue
			}
			if goCommentGroupContains(set, value.Doc, offset) {
				return goDeclaration{variable: true}, true
			}
			for _, specification := range value.Specs {
				variable, ok := specification.(*ast.ValueSpec)
				if ok && goCommentGroupContains(set, variable.Doc, offset) {
					return goDeclaration{variable: true}, true
				}
			}
		}
	}
	return goDeclaration{}, false
}

func goCommentGroupContains(set *token.FileSet, group *ast.CommentGroup, offset int) bool {
	if group == nil {
		return false
	}
	for _, comment := range group.List {
		if set.Position(comment.Pos()).Offset == offset {
			return true
		}
	}
	return false
}

func goBuildAllowed(data []byte, packageOffset, buildCount int, annotation sourceAnnotation) bool {
	if buildCount != 1 || !sourceLineStart(data, annotation.offset) || annotation.offset >= packageOffset || !strings.HasPrefix(annotation.text, "//go:build ") {
		return false
	}
	if _, err := constraint.Parse(annotation.text); err != nil {
		return false
	}
	index := nextLine(data, lineEnd(data, annotation.offset))
	for index < packageOffset {
		end := lineEnd(data, index)
		if sourceLineBlank(data[index:end]) {
			return true
		}
		index = nextLine(data, end)
	}
	return false
}

func goGenerateAllowed(data []byte, annotation sourceAnnotation) bool {
	if !sourceLineStart(data, annotation.offset) || !strings.HasPrefix(annotation.text, "//go:generate ") {
		return false
	}
	value := strings.TrimPrefix(annotation.text, "//go:generate ")
	return value != "" && strings.TrimSpace(value) == value
}

func goLineAllowed(data []byte, annotation sourceAnnotation) bool {
	if !sourceLineStart(data, annotation.offset) || !strings.HasPrefix(annotation.text, "//line ") {
		return false
	}
	value := strings.TrimPrefix(annotation.text, "//line ")
	if value == "" || strings.ContainsAny(value, " \t\r\n") {
		return false
	}
	last := strings.LastIndexByte(value, ':')
	if last < 0 || !goPositiveDecimal(value[last+1:]) {
		return false
	}
	before := value[:last]
	second := strings.LastIndexByte(before, ':')
	if second >= 0 {
		return second > 0 && goPositiveDecimal(before[second+1:]) && !strings.Contains(before[:second], ":")
	}
	return before != ""
}

func goPositiveDecimal(value string) bool {
	if value == "" || value[0] == '0' {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func goDebugAllowed(path string, data []byte, parsed *ast.File, packageOffset int, annotation sourceAnnotation) bool {
	if !sourceLineStart(data, annotation.offset) || annotation.offset >= packageOffset || !strings.HasPrefix(annotation.text, "//go:debug ") {
		return false
	}
	if parsed.Name == nil || (parsed.Name.Name != "main" && !strings.HasSuffix(path, "_test.go")) {
		return false
	}
	return goDebugDirective.MatchString(strings.TrimPrefix(annotation.text, "//go:debug "))
}

func goExportAllowed(text string, declaration *ast.FuncDecl) bool {
	if declaration == nil || declaration.Name == nil || !strings.HasPrefix(text, "//export ") {
		return false
	}
	name := strings.TrimPrefix(text, "//export ")
	return goDirectiveField.MatchString(name) && name == declaration.Name.Name
}

var goDebugDirective = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*=[A-Za-z0-9_.-]+$`)
var goDirectiveField = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_./-]*$`)

var goFunctionDirective = map[string]bool{
	"//go:nointerface":        true,
	"//go:noescape":           true,
	"//go:nosplit":            true,
	"//go:noinline":           true,
	"//go:norace":             true,
	"//go:nocheckptr":         true,
	"//go:nowritebarrier":     true,
	"//go:nowritebarrierrec":  true,
	"//go:yeswritebarrierrec": true,
	"//go:systemstack":        true,
	"//go:cgo_unsafe_args":    true,
	"//go:uintptrescapes":     true,
	"//go:uintptrkeepalive":   true,
	"//go:registerparams":     true,
}

func goDirectiveFieldsAllowed(text, prefix string, minimum, maximum int) bool {
	if !strings.HasPrefix(text, prefix) {
		return false
	}
	value := strings.TrimPrefix(text, prefix)
	if value == "" || strings.TrimSpace(value) != value {
		return false
	}
	fields := strings.Fields(value)
	if len(fields) < minimum || len(fields) > maximum || strings.Join(fields, " ") != value {
		return false
	}
	for _, field := range fields {
		if !goDirectiveField.MatchString(field) {
			return false
		}
	}
	return true
}

func goEmbedArgumentsAllowed(value string) bool {
	if value == "" || strings.TrimSpace(value) != value {
		return false
	}
	for index := 0; index < len(value); {
		end, ok := goEmbedArgumentEnd(value, index)
		if !ok {
			return false
		}
		index, ok = goEmbedNextArgument(value, end)
		if !ok {
			return false
		}
		if index == len(value) {
			return true
		}
	}
	return false
}

func goEmbedArgumentEnd(value string, index int) (int, bool) {
	switch value[index] {
	case '"':
		return goQuotedEmbedArgumentEnd(value, index)
	case '`':
		return goRawEmbedArgumentEnd(value, index)
	default:
		return goBareEmbedArgumentEnd(value, index)
	}
}

func goQuotedEmbedArgumentEnd(value string, index int) (int, bool) {
	end := index + 1
	for end < len(value) {
		if value[end] == '\\' {
			end += 2
			continue
		}
		if value[end] == '"' {
			decoded, err := strconv.Unquote(value[index : end+1])
			return end + 1, err == nil && goEmbedPatternAllowed(decoded)
		}
		end++
	}
	return 0, false
}

func goRawEmbedArgumentEnd(value string, index int) (int, bool) {
	end := strings.IndexByte(value[index+1:], '`')
	if end < 0 {
		return 0, false
	}
	end += index + 1
	return end + 1, end > index+1 && goEmbedPatternAllowed(value[index+1:end])
}

func goBareEmbedArgumentEnd(value string, index int) (int, bool) {
	end := index
	for end < len(value) && value[end] != ' ' {
		if value[end] == '\t' || value[end] == '\r' || value[end] == '\n' {
			return 0, false
		}
		end++
	}
	return end, end > index && goEmbedPatternAllowed(value[index:end])
}

func goEmbedNextArgument(value string, index int) (int, bool) {
	if index == len(value) {
		return index, true
	}
	if value[index] != ' ' {
		return 0, false
	}
	for index < len(value) && value[index] == ' ' {
		index++
	}
	return index, index < len(value)
}

func goEmbedPatternAllowed(pattern string) bool {
	if pattern == "" || strings.ContainsAny(pattern, "\t\r\n") || strings.HasPrefix(pattern, "/") || strings.HasSuffix(pattern, "/") || strings.Contains(pattern, "//") {
		return false
	}
	for _, segment := range strings.Split(strings.TrimPrefix(pattern, "all:"), "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

func lineEnd(data []byte, index int) int {
	for index < len(data) && data[index] != '\r' && data[index] != '\n' {
		index++
	}
	return index
}

func nextLine(data []byte, index int) int {
	if index < len(data) && data[index] == '\r' {
		index++
	}
	if index < len(data) && data[index] == '\n' {
		index++
	}
	return index
}
