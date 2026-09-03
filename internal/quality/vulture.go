package quality

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

const pythonVultureProtocolVersion = 1

const pythonVultureVersion = "2.16"

const pythonVultureInputMaximumBytes = 4 << 20

const pythonVultureAdapter = `import ast,json,os,re,sys
from collections import defaultdict
P=1
M=4194304
S=4096
R=re.compile(r"^[A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)*$")
o={"protocol":P,"tool_version":"","covered":[],"diagnostics":[],"resolved":[],"problems":[],"error":""}
def e(x):
 o["error"]=str(x)[:S]
def q(x):
 if not isinstance(x,str) or not x or len(x)>S or "\x00" in x:raise ValueError("invalid string")
 return x
def n(x):
 return isinstance(x,str) and bool(R.fullmatch(x))
def p(x):
 q(x)
 if x.startswith("/") or "\\" in x or any(y in ("",".","..") for y in x.split("/")):raise ValueError("invalid path")
 return x
def c(f,node,name):
 d=getattr(node,"decorator_list",())
 return (f["path"],d[0].lineno if d else node.lineno,node.end_lineno,name)
def ts(node):
 if isinstance(node,(ast.Tuple,ast.List)):
  for x in node.elts:yield from ts(x)
 elif isinstance(node,ast.Name):yield node
def ds(f,body,name):
 z=[]
 for x in body:
  if isinstance(x,(ast.FunctionDef,ast.AsyncFunctionDef,ast.ClassDef)) and x.name==name:z.append(("d",x))
  elif isinstance(x,(ast.Assign,ast.AnnAssign)):
   a=x.targets if isinstance(x,ast.Assign) else [x.target]
   for t in a:
    for y in ts(t):
     if y.id==name:z.append(("d",y))
  elif isinstance(x,ast.ImportFrom):
   for a in x.names:
    if (a.asname or a.name.partition(".")[0])==name:z.append(("i",x,a))
 return z
def rm(f,mod,parts,seen):
 k=(mod,".".join(parts))
 if k in seen:raise ValueError("reference reexport cycle")
 seen=seen|{k}
 z=mods.get(mod,[])
 if not z:raise ValueError("module has no runtime .py definition")
 if len(z)!=1:raise ValueError("module is ambiguous")
 f=z[0]; d=ds(f,f["tree"].body,parts[0])
 if len(d)!=1:raise ValueError("symbol is stale or ambiguous")
 x=d[0]
 if x[0]=="i":
  node,a=x[1:]
  if not node.module:raise ValueError("reexport has no module")
  b=[]
  if node.level:
   if not f["package"]:raise ValueError("relative reexport has no package")
   b=f["package"].split(".")
   if len(b)<node.level-1:raise ValueError("relative reexport escapes package")
   b=b[:len(b)-node.level+1]
  t=".".join(b+[node.module])
  return [c(f,node,a.asname or a.name.partition(".")[0])]+rm(f,t,a.name.split(".")+parts[1:],seen)
 node=x[1]; r=[c(f,node,parts[0])]
 if len(parts)==1:return r
 if not isinstance(node,ast.ClassDef):raise ValueError("symbol has no class member")
 d=ds(f,node.body,parts[1])
 if len(d)!=1 or d[0][0]!="d":raise ValueError("class member is stale or ambiguous")
 return r+cm(f,d[0][1],parts[1:])
def cm(f,node,parts):
 r=[c(f,node,parts[0])]
 if len(parts)==1:return r
 if not isinstance(node,ast.ClassDef):raise ValueError("symbol has no class member")
 d=ds(f,node.body,parts[1])
 if len(d)!=1 or d[0][0]!="d":raise ValueError("class member is stale or ambiguous")
 return r+cm(f,d[0][1],parts[1:])
try:
 b=sys.stdin.buffer.read(M+1)
 if len(b)>M:raise ValueError("input exceeds limit")
 x=json.loads(b)
 if not isinstance(x,dict) or set(x)!={"protocol","tool_version","root","files","references"}:raise ValueError("invalid request")
 if type(x["protocol"]) is not int or x["protocol"]!=P or not isinstance(x["tool_version"],str):raise ValueError("invalid protocol")
 root=q(x["root"])
 if not os.path.isabs(root):raise ValueError("root is not absolute")
 root=os.path.realpath(root)
 if not os.path.isdir(root):raise ValueError("root is not a directory")
 import importlib.metadata
 o["tool_version"]=importlib.metadata.version("vulture")
 if o["tool_version"]!=x["tool_version"]:raise ValueError("Vulture version mismatch")
 if not isinstance(x["files"],list) or not x["files"]:raise ValueError("invalid files")
 fs=[]; seen=set()
 for f in x["files"]:
  if not isinstance(f,dict) or set(f)!={"path","module","package"}:raise ValueError("invalid file")
  path=p(f["path"])
  if not path.endswith((".py",".pyi")) or path in seen:raise ValueError("invalid file path")
  seen.add(path)
  for k in ("module","package"):
   if not isinstance(f[k],str) or (f[k] and not n(f[k])):raise ValueError("invalid module")
  h=os.path.realpath(os.path.join(root,*path.split("/")))
  if os.path.commonpath((root,h))!=root or not os.path.isfile(h):raise ValueError("file escapes root")
  with open(h,encoding="utf-8") as g:s=g.read()
  fs.append({"path":path,"module":f["module"],"package":f["package"],"tree":ast.parse(s,filename=path,type_comments=True),"source":s})
 if not isinstance(x["references"],list):raise ValueError("invalid references")
 rs=[]; ids=set()
 for r in x["references"]:
  if not isinstance(r,dict) or set(r)!={"id","module","symbol"}:raise ValueError("invalid reference")
  i=q(r["id"])
  if i in ids or not n(r["module"]) or not n(r["symbol"]):raise ValueError("invalid reference")
  ids.add(i);rs.append(r)
 mods={}
 for f in fs:
  if f["path"].endswith(".py") and f["module"]:mods.setdefault(f["module"],[]).append(f)
 import vulture.core as vc
 vc.noqa.parse_noqa=lambda _:defaultdict(set)
 v=vc.Vulture()
 for f in fs:
  v.scan(f["source"],filename=f["path"]);o["covered"].append(f["path"])
 keep=set()
 for r in rs:
  try:
   keep.update(rm(None,r["module"],r["symbol"].split("."),set()));o["resolved"].append(r["id"])
  except Exception as z:o["problems"].append({"id":r["id"],"message":str(z)[:S]})
 for z in v.get_unused_code(min_confidence=60):
  a=(str(z.filename),z.first_lineno,z.last_lineno,z.name)
  if a in keep:continue
  if not z.name or len(z.name)>S or not z.message or len(z.message)>S:raise ValueError("invalid Vulture diagnostic")
  o["diagnostics"].append({"path":a[0],"line":a[1],"end":a[2],"name":z.name,"kind":z.typ,"confidence":z.confidence,"message":z.message})
except Exception as z:e(z)
sys.stdout.write(json.dumps(o,sort_keys=True,separators=(",",":")))`

type pythonVultureFile struct {
	Path    string `json:"path"`
	Module  string `json:"module"`
	Package string `json:"package"`
}

type pythonVultureReference struct {
	ID     string `json:"id"`
	Module string `json:"module"`
	Symbol string `json:"symbol"`
}

type pythonVultureRequest struct {
	Protocol    int                      `json:"protocol"`
	ToolVersion string                   `json:"tool_version"`
	Root        string                   `json:"root"`
	Files       []pythonVultureFile      `json:"files"`
	References  []pythonVultureReference `json:"references"`
}

type pythonVultureReferenceOrigin struct {
	Path    string
	Line    int
	Subject string
}

type pythonVultureDiagnostic struct {
	Path       string `json:"path"`
	Line       int    `json:"line"`
	End        int    `json:"end"`
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	Confidence int    `json:"confidence"`
	Message    string `json:"message"`
}

type pythonVultureProblem struct {
	ID      string `json:"id"`
	Message string `json:"message"`
}

type pythonVultureResponseWire struct {
	Protocol    *int                       `json:"protocol"`
	ToolVersion *string                    `json:"tool_version"`
	Covered     *[]string                  `json:"covered"`
	Diagnostics *[]pythonVultureDiagnostic `json:"diagnostics"`
	Resolved    *[]string                  `json:"resolved"`
	Problems    *[]pythonVultureProblem    `json:"problems"`
	Error       *string                    `json:"error"`
}

type pythonVultureResponse struct {
	Covered     []string
	Diagnostics []pythonVultureDiagnostic
	Resolved    []string
	Problems    []pythonVultureProblem
	Error       string
}

func pythonVultureCommand(repo repository.Repository, project repository.PythonProject) (policy.Command, error) {
	interpreter := repo.PythonTool()
	if !filepath.IsAbs(interpreter) {
		return policy.Command{}, fmt.Errorf("policy Python interpreter is not absolute")
	}
	files := make([]pythonVultureFile, 0, len(project.Files))
	for _, source := range project.Files {
		module, packageName := repository.PythonModuleName(project, source)
		files = append(files, pythonVultureFile{Path: source, Module: module, Package: packageName})
	}
	sort.Slice(files, func(left, right int) bool { return files[left].Path < files[right].Path })
	references, _ := pythonVultureReferences(repo, project)
	request := pythonVultureRequest{
		Protocol: pythonVultureProtocolVersion, ToolVersion: pythonVultureVersion, Root: repo.Root,
		Files: files, References: references,
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		return policy.Command{}, fmt.Errorf("encode Vulture request: %w", err)
	}
	if len(encoded) > pythonVultureInputMaximumBytes {
		return policy.Command{}, fmt.Errorf("vulture request exceeds %d bytes", pythonVultureInputMaximumBytes)
	}
	return policy.Command{
		Name:              "policy-vulture-dead-code-" + pythonQualityProjectName(project.Root),
		Provides:          []string{"dead-code"},
		Argv:              []string{interpreter, "-I", "-c", pythonVultureAdapter},
		Cwd:               ".",
		Modules:           pythonQualityModules(repo, project.Files),
		RunOn:             []string{"check", "gate"},
		TimeoutSeconds:    int(pythonQualityBudget.Seconds()),
		Managed:           true,
		SealedEnvironment: true,
		Stdin:             append(encoded, '\n'),
	}, nil
}

func pythonVultureReferences(repo repository.Repository, project repository.PythonProject) ([]pythonVultureReference, map[string]pythonVultureReferenceOrigin) {
	references := []pythonVultureReference{}
	origins := map[string]pythonVultureReferenceOrigin{}
	configPath := pythonVultureConfigPath(repo)
	for _, reference := range repo.Config.Scope.PythonDynamicReferences {
		if reference.Project != project.Manifest {
			continue
		}
		id := pythonVultureConfigReferenceID(reference)
		references = append(references, pythonVultureReference{ID: id, Module: reference.Module, Symbol: reference.Symbol})
		origins[id] = pythonVultureReferenceOrigin{Path: configPath, Subject: id}
	}
	for _, reference := range project.DynamicReferences {
		id := pythonVultureManifestReferenceID(project, reference)
		references = append(references, pythonVultureReference{ID: id, Module: reference.Module, Symbol: reference.Symbol})
		origins[id] = pythonVultureReferenceOrigin{Path: project.Manifest, Line: reference.Line, Subject: id}
	}
	sort.Slice(references, func(left, right int) bool { return references[left].ID < references[right].ID })
	return references, origins
}

func pythonVultureConfigReferenceID(reference policy.PythonDynamicReference) string {
	return "config:" + reference.Project + ":" + reference.Module + ":" + reference.Symbol
}

func pythonVultureManifestReferenceID(project repository.PythonProject, reference repository.PythonDynamicReference) string {
	return "manifest:" + project.Manifest + ":" + reference.Table + ":" + reference.Name + ":" + reference.Module + ":" + reference.Symbol
}

func pythonVultureConfigPath(repo repository.Repository) string {
	if repo.Config.ConfigPath != "" {
		if path, err := repo.NormalizePath(repo.Config.ConfigPath); err == nil {
			return path
		}
	}
	return policy.ConfigFilename
}

func pythonQualitySelectedManifests(repo repository.Repository, selected []string) map[string]bool {
	manifests := map[string]bool{}
	for _, candidate := range selected {
		normalized, err := repo.NormalizePath(candidate)
		if err != nil || filepath.Base(normalized) != "pyproject.toml" {
			continue
		}
		manifests[normalized] = true
	}
	return manifests
}

func pythonQualityDynamicReferenceInventoryFindings(repo repository.Repository, message string) []policy.Finding {
	findings := make([]policy.Finding, 0, len(repo.Config.Scope.PythonDynamicReferences))
	configPath := pythonVultureConfigPath(repo)
	for _, reference := range repo.Config.Scope.PythonDynamicReferences {
		findings = append(findings, policy.Finding{
			Check: "policy.pythonDynamicReference", Path: configPath, Subject: pythonVultureConfigReferenceID(reference),
			Message: "Python dynamic reference cannot determine its contained project: " + message,
		})
	}
	return findings
}

func pythonQualityDynamicReferenceProjects(repo repository.Repository, projects map[string]repository.PythonProject, selectedByProject map[string][]string, selectedManifests map[string]bool) ([]policy.Finding, map[string][]string) {
	findings, referenceOnly, unrunnable := pythonQualityConfigDynamicReferenceProjects(repo, projects, selectedByProject)
	return append(findings, pythonQualityInferredDynamicReferenceFindings(projects, selectedByProject, selectedManifests, referenceOnly, unrunnable)...), referenceOnly
}

func pythonQualityConfigDynamicReferenceProjects(repo repository.Repository, projects map[string]repository.PythonProject, selectedByProject map[string][]string) ([]policy.Finding, map[string][]string, map[string]bool) {
	findings := []policy.Finding{}
	referenceOnly := map[string][]string{}
	unrunnable := map[string]bool{}
	configPath := pythonVultureConfigPath(repo)
	for _, reference := range repo.Config.Scope.PythonDynamicReferences {
		project, found := projects[reference.Project]
		if !found {
			findings = append(findings, policy.Finding{
				Check: "policy.pythonDynamicReference", Path: configPath, Subject: pythonVultureConfigReferenceID(reference),
				Message: "Python dynamic reference names no current contained Python project",
			})
			continue
		}
		if _, selected := selectedByProject[project.Manifest]; selected {
			continue
		}
		if !pythonQualityProjectHasRuntimeSource(project) {
			findings = append(findings, policy.Finding{
				Check: "policy.pythonDynamicReference", Path: configPath, Subject: pythonVultureConfigReferenceID(reference),
				Message: "Python dynamic reference names a project with no runtime .py definitions",
			})
			unrunnable[project.Manifest] = true
			continue
		}
		referenceOnly[project.Manifest] = nil
	}
	return findings, referenceOnly, unrunnable
}

func pythonQualityInferredDynamicReferenceFindings(projects map[string]repository.PythonProject, selectedByProject map[string][]string, selectedManifests map[string]bool, referenceOnly map[string][]string, unrunnable map[string]bool) []policy.Finding {
	findings := []policy.Finding{}
	for _, project := range projects {
		if len(project.DynamicReferences) == 0 || !pythonQualityChecksInferredDynamicReferences(project.Manifest, selectedByProject, selectedManifests, referenceOnly, unrunnable) {
			continue
		}
		_, selected := selectedByProject[project.Manifest]
		if pythonQualityProjectHasRuntimeSource(project) {
			if !selected {
				referenceOnly[project.Manifest] = nil
			}
			continue
		}
		for _, reference := range project.DynamicReferences {
			findings = append(findings, policy.Finding{
				Check: "policy.pythonDynamicReference", Path: project.Manifest, Line: reference.Line,
				Subject: pythonVultureManifestReferenceID(project, reference),
				Message: "Python dynamic reference names a project with no runtime .py definitions",
			})
		}
	}
	return findings
}

func pythonQualityChecksInferredDynamicReferences(manifest string, selectedByProject map[string][]string, selectedManifests map[string]bool, referenceOnly map[string][]string, unrunnable map[string]bool) bool {
	if _, selected := selectedByProject[manifest]; selected {
		return true
	}
	if selectedManifests[manifest] {
		return true
	}
	if _, referenced := referenceOnly[manifest]; referenced {
		return true
	}
	return unrunnable[manifest]
}

func pythonQualityProjectHasRuntimeSource(project repository.PythonProject) bool {
	for _, source := range project.Files {
		if strings.HasSuffix(source, ".py") {
			return true
		}
	}
	return false
}

func pythonVultureFindings(repo repository.Repository, project repository.PythonProject, data []byte) []policy.Finding {
	response, err := parsePythonVultureResponse(data)
	if err != nil {
		return pythonVultureCoverage(project.Files, "the policy-owned Vulture output cannot be used: "+err.Error())
	}
	if response.Error != "" {
		return pythonVultureCoverage(project.Files, "the policy-owned Vulture analysis failed: "+response.Error)
	}
	references, origins := pythonVultureReferences(repo, project)
	if err := validatePythonVultureResponse(project.Files, references, response); err != nil {
		return pythonVultureCoverage(project.Files, "the policy-owned Vulture output cannot be used: "+err.Error())
	}
	findings := pythonVultureReferenceFindings(response.Problems, origins)
	for _, diagnostic := range response.Diagnostics {
		findings = append(findings, policy.Finding{
			Check: "quality.deadCode", Path: diagnostic.Path, Line: diagnostic.Line,
			Subject: pythonVultureSubject(diagnostic), Message: pythonVultureMessage(diagnostic),
		})
	}
	return findings
}

func parsePythonVultureResponse(data []byte) (pythonVultureResponse, error) {
	if len(data) > pythonStructuredOutputMaximumBytes {
		return pythonVultureResponse{}, fmt.Errorf("output exceeds %d bytes", pythonStructuredOutputMaximumBytes)
	}
	wire, err := decodePythonVultureResponse(data)
	if err != nil {
		return pythonVultureResponse{}, err
	}
	return pythonVultureResponseFromWire(wire)
}

func decodePythonVultureResponse(data []byte) (pythonVultureResponseWire, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	wire := pythonVultureResponseWire{}
	if err := decoder.Decode(&wire); err != nil {
		return pythonVultureResponseWire{}, fmt.Errorf("malformed JSON: %w", err)
	}
	if err := pythonVultureTrailingJSONError(decoder); err != nil {
		return pythonVultureResponseWire{}, err
	}
	return wire, nil
}

func pythonVultureTrailingJSONError(decoder *json.Decoder) error {
	var trailing any
	err := decoder.Decode(&trailing)
	if err == io.EOF {
		return nil
	}
	if err == nil {
		return fmt.Errorf("JSON has trailing data")
	}
	return fmt.Errorf("malformed trailing JSON: %w", err)
}

func pythonVultureResponseFromWire(wire pythonVultureResponseWire) (pythonVultureResponse, error) {
	if !pythonVultureResponseWireComplete(wire) {
		return pythonVultureResponse{}, fmt.Errorf("output omits required fields")
	}
	if *wire.Protocol != pythonVultureProtocolVersion {
		return pythonVultureResponse{}, fmt.Errorf("protocol is not %d", pythonVultureProtocolVersion)
	}
	if *wire.ToolVersion != pythonVultureVersion {
		return pythonVultureResponse{}, fmt.Errorf("vulture version is not %s", pythonVultureVersion)
	}
	if len(*wire.Error) > pythonStructuredMessageMaximumBytes {
		return pythonVultureResponse{}, fmt.Errorf("error exceeds %d bytes", pythonStructuredMessageMaximumBytes)
	}
	return pythonVultureResponse{
		Covered: *wire.Covered, Diagnostics: *wire.Diagnostics, Resolved: *wire.Resolved, Problems: *wire.Problems, Error: *wire.Error,
	}, nil
}

func pythonVultureResponseWireComplete(wire pythonVultureResponseWire) bool {
	return wire.Protocol != nil && wire.ToolVersion != nil && wire.Covered != nil && wire.Diagnostics != nil &&
		wire.Resolved != nil && wire.Problems != nil && wire.Error != nil
}

func validatePythonVultureResponse(files []string, references []pythonVultureReference, response pythonVultureResponse) error {
	if err := validatePythonVultureCovered(files, response.Covered); err != nil {
		return err
	}
	if err := validatePythonVultureReferences(references, response.Resolved, response.Problems); err != nil {
		return err
	}
	return validatePythonVultureDiagnostics(files, response.Diagnostics)
}

func validatePythonVultureCovered(files, covered []string) error {
	expected := map[string]bool{}
	for _, path := range files {
		expected[path] = true
	}
	actual := map[string]bool{}
	for _, path := range covered {
		if !expected[path] || actual[path] {
			return fmt.Errorf("covered paths are not an exact project inventory")
		}
		actual[path] = true
	}
	if len(actual) != len(expected) {
		return fmt.Errorf("covered paths omit governed Python source")
	}
	return nil
}

func validatePythonVultureReferences(expected []pythonVultureReference, resolved []string, problems []pythonVultureProblem) error {
	remaining := map[string]bool{}
	for _, reference := range expected {
		if remaining[reference.ID] {
			return fmt.Errorf("policy dynamic references are not unique")
		}
		remaining[reference.ID] = true
	}
	for _, id := range resolved {
		if !remaining[id] {
			return fmt.Errorf("resolved references are not an exact request")
		}
		delete(remaining, id)
	}
	for _, problem := range problems {
		if !remaining[problem.ID] || strings.TrimSpace(problem.Message) == "" || len(problem.Message) > pythonStructuredMessageMaximumBytes {
			return fmt.Errorf("problem references are not an exact request")
		}
		delete(remaining, problem.ID)
	}
	if len(remaining) != 0 {
		return fmt.Errorf("reference result omits requested references")
	}
	return nil
}

func validatePythonVultureDiagnostics(files []string, diagnostics []pythonVultureDiagnostic) error {
	if len(diagnostics) > pythonStructuredDiagnosticMaximum {
		return fmt.Errorf("output contains more than %d diagnostics", pythonStructuredDiagnosticMaximum)
	}
	known := pythonVultureKnownFiles(files)
	seen := map[string]bool{}
	for _, diagnostic := range diagnostics {
		if !pythonVultureDiagnosticIsValid(known, diagnostic) {
			return fmt.Errorf("diagnostic is invalid")
		}
		key := pythonVultureDiagnosticIdentity(diagnostic)
		if seen[key] {
			return fmt.Errorf("diagnostic appears more than once")
		}
		seen[key] = true
	}
	return nil
}

func pythonVultureKnownFiles(files []string) map[string]bool {
	known := map[string]bool{}
	for _, path := range files {
		known[path] = true
	}
	return known
}

func pythonVultureDiagnosticIsValid(known map[string]bool, diagnostic pythonVultureDiagnostic) bool {
	return known[diagnostic.Path] && diagnostic.Line >= 1 && diagnostic.End >= diagnostic.Line && diagnostic.Name != "" &&
		len(diagnostic.Name) <= pythonStructuredMessageMaximumBytes && pythonVultureDiagnosticKind(diagnostic.Kind) &&
		diagnostic.Confidence >= 60 && diagnostic.Confidence <= 100 && strings.TrimSpace(diagnostic.Message) != "" &&
		len(diagnostic.Message) <= pythonStructuredMessageMaximumBytes
}

func pythonVultureDiagnosticIdentity(diagnostic pythonVultureDiagnostic) string {
	return strings.Join([]string{
		diagnostic.Path, strconv.Itoa(diagnostic.Line), strconv.Itoa(diagnostic.End), diagnostic.Name, diagnostic.Kind,
		strconv.Itoa(diagnostic.Confidence), diagnostic.Message,
	}, "\x00")
}

func pythonVultureDiagnosticKind(kind string) bool {
	return map[string]bool{
		"attribute": true, "class": true, "function": true, "import": true, "method": true,
		"property": true, "unreachable_code": true, "variable": true,
	}[kind]
}

func pythonVultureReferenceFindings(problems []pythonVultureProblem, origins map[string]pythonVultureReferenceOrigin) []policy.Finding {
	findings := make([]policy.Finding, 0, len(problems))
	for _, problem := range problems {
		origin := origins[problem.ID]
		findings = append(findings, policy.Finding{
			Check: "policy.pythonDynamicReference", Path: origin.Path, Line: origin.Line, Subject: origin.Subject,
			Message: "Python dynamic reference cannot resolve exactly: " + problem.Message,
		})
	}
	return findings
}

func pythonVultureCoverage(files []string, message string) []policy.Finding {
	return pythonQualityCoverage(files, "quality.deadCodeCoverage", "vulture", message)
}

func pythonVultureSubject(diagnostic pythonVultureDiagnostic) string {
	identity := strings.Join([]string{
		diagnostic.Path, strconv.Itoa(diagnostic.Line), strconv.Itoa(diagnostic.End), diagnostic.Name, diagnostic.Kind,
		strconv.Itoa(diagnostic.Confidence), diagnostic.Message,
	}, "\x00")
	digest := sha256.Sum256([]byte(identity))
	return "vulture:" + hex.EncodeToString(digest[:])
}

func pythonVultureMessage(diagnostic pythonVultureDiagnostic) string {
	line := strconv.Itoa(diagnostic.Line)
	if diagnostic.End != diagnostic.Line {
		line += "-" + strconv.Itoa(diagnostic.End)
	}
	return "lines " + line + ": " + diagnostic.Message + " (" + diagnostic.Kind + ", " + strconv.Itoa(diagnostic.Confidence) + "% confidence)"
}
