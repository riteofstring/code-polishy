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
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/pythonfacts"
	"github.com/riteofstring/code-polishy/internal/repository"
)

const pythonVultureProtocolVersion = 10

const pythonVultureVersion = "2.16"

const pythonVultureInputMaximumBytes = 4 << 20

const pythonVultureAdapter = `import ast,json,os,pkgutil,re,sys,time
from collections import defaultdict
from source_parser import parse_source
from type_facts import type_facts
from type_resolver import typed_dict_reads
from repository_contracts import framework_members
P=10
M=4194304
S=4096
R=re.compile(r"^[A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)*$")
o={"protocol":P,"tool_version":"","covered":[],"diagnostics":[],"resolved":[],"problems":[],"facts_error":"","error":"","reachability":[],"timings":[]}
started=time.perf_counter_ns();lap=started
def tm(name):
 global lap
 now=time.perf_counter_ns();o["timings"].append({"name":name,"duration_ns":now-lap});lap=now
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
 line=d[0].lineno if d else node.lineno
 end=line if isinstance(node,(ast.Assign,ast.AnnAssign,ast.Name,ast.Attribute)) else node.end_lineno
 return (f["path"],line,end,name)
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
def ib(f):
 r={}
 for x in f["tree"].body:
  if isinstance(x,ast.Import):
   for a in x.names:r[a.asname or a.name.partition(".")[0]]=a.name if a.asname else a.name.partition(".")[0]
  elif isinstance(x,ast.ImportFrom):
   m=fm(f,x)
   if m:
    for a in x.names:
     if a.name!="*":r[a.asname or a.name]=m+"."+a.name
 return r
def fm(f,x):
 if not isinstance(x,ast.ImportFrom):return ""
 b=[]
 if x.level:
  if not f["package"]:return ""
  b=f["package"].split(".")
  if len(b)<x.level-1:return ""
  b=b[:len(b)-x.level+1]
 if x.module:b.append(x.module)
 return ".".join(b)
def en(x,b):
 if isinstance(x,ast.Name):return b.get(x.id,x.id)
 if isinstance(x,ast.Attribute):
  z=en(x.value,b)
  return z+"."+x.attr if z else ""
 return ""
def ka(f):
 k=set();b=ib(f);typed=set()
 protocols={
  "ast.NodeVisitor":lambda n:n.startswith("visit_"),
  "ast.NodeTransformer":lambda n:n.startswith("visit_"),
  "urllib.request.BaseHandler":lambda n:n=="redirect_request" or n in ("default_open","unknown_open","proxy_open") or n.startswith("http_error_") or n.endswith("_open"),
  "urllib.request.HTTPRedirectHandler":lambda n:n=="redirect_request" or n.startswith("http_error_"),
  "html.parser.HTMLParser":lambda n:n in ("handle_starttag","handle_startendtag","handle_endtag","handle_data","handle_entityref","handle_charref","handle_comment","handle_decl","unknown_decl"),
 }
 for x in ast.walk(f["tree"]):
  if isinstance(x,ast.ClassDef):
   tests=[protocols.get(en(y,b)) for y in x.bases]
   tests=[y for y in tests if y]
   if tests:
    for y in x.body:
     if isinstance(y,(ast.FunctionDef,ast.AsyncFunctionDef)) and any(z(y.name) for z in tests):k.add(c(f,y,y.name))
  if isinstance(x,(ast.FunctionDef,ast.AsyncFunctionDef)) and x.name in ("__exit__","__aexit__"):
   a=x.args
   for y in a.posonlyargs+a.args+a.kwonlyargs+([a.vararg] if a.vararg else [])+([a.kwarg] if a.kwarg else []):k.add(c(f,y,y.arg))
  if isinstance(x,(ast.Assign,ast.AnnAssign)):
   value=x.value
   if isinstance(value,ast.Call) and en(value.func,b)=="zipfile.ZipInfo":
    targets=x.targets if isinstance(x,ast.Assign) else [x.target]
    for y in targets:
     if isinstance(y,ast.Name):typed.add(y.id)
  if isinstance(x,ast.Attribute) and isinstance(x.ctx,ast.Store) and x.attr in ("__cause__","__context__","__traceback__","__suppress_context__"):k.add(c(f,x,x.attr))
 for x in ast.walk(f["tree"]):
  if isinstance(x,ast.Attribute) and isinstance(x.ctx,ast.Store) and isinstance(x.value,ast.Name) and x.value.id in typed:k.add(c(f,x,x.attr))
 return k
def fc(mod,parts):
 z=mods.get(mod,[])
 if not z:raise ValueError("write module has no runtime .py definition")
 if len(z)!=1:raise ValueError("write module is ambiguous")
 f=z[0];body=f["tree"].body;node=None
 for i,name in enumerate(parts):
  d=ds(f,body,name)
  if len(d)!=1 or d[0][0]!="d":raise ValueError("write callable is stale or ambiguous")
  node=d[0][1]
  if i<len(parts)-1:
   if not isinstance(node,ast.ClassDef):raise ValueError("write callable cannot be resolved statically")
   body=node.body
 if not isinstance(node,(ast.FunctionDef,ast.AsyncFunctionDef)):raise ValueError("write target is not a callable")
 return f,node
def wn(node):
 for x in ast.iter_child_nodes(node):
  if isinstance(x,(ast.FunctionDef,ast.AsyncFunctionDef,ast.ClassDef,ast.Lambda)):continue
  yield x;yield from wn(x)
try:
 b=sys.stdin.buffer.read(M+1)
 if len(b)>M:raise ValueError("input exceeds limit")
 x=json.loads(b)
 if not isinstance(x,dict) or set(x)!={"protocol","tool_version","root","files","targets","complete","references","backends","attributes","contracts"}:raise ValueError("invalid request")
 if type(x["protocol"]) is not int or x["protocol"]!=P or not isinstance(x["tool_version"],str):raise ValueError("invalid protocol")
 root=q(x["root"])
 if not os.path.isabs(root):raise ValueError("root is not absolute")
 root=os.path.realpath(root)
 if not os.path.isdir(root):raise ValueError("root is not a directory")
 import importlib.metadata
 o["tool_version"]=importlib.metadata.version("vulture")
 if o["tool_version"]!=x["tool_version"]:raise ValueError("Vulture version mismatch")
 tm("request-validation")
 if not isinstance(x["files"],list) or not x["files"]:raise ValueError("invalid files")
 fs=[]; seen=set();source_bytes=0;ast_nodes=0
 for f in x["files"]:
  if not isinstance(f,dict) or set(f)!={"path","module","package"}:raise ValueError("invalid file")
  path=p(f["path"])
  if not path.endswith((".py",".pyi")) or path in seen:raise ValueError("invalid file path")
  seen.add(path)
  for k in ("module","package"):
   if not isinstance(f[k],str) or (f[k] and not n(f[k])):raise ValueError("invalid module")
  h=os.path.realpath(os.path.join(root,*path.split("/")))
  if os.path.commonpath((root,h))!=root or not os.path.isfile(h):raise ValueError("file escapes root")
  try:
   s,tree=_vulture_read_source(path,h)
   source_bytes+=len(s.encode("utf-8"));ast_nodes+=sum(1 for _ in ast.walk(tree))
   if source_bytes>512*1024*1024 or ast_nodes>2000000 or len(fs)>=65536:raise ValueError("Python project exceeds its source or AST resource boundary")
  except Exception as z:
   o["facts_error"]=str(z)[:S];raise
  fs.append({"path":path,"module":f["module"],"package":f["package"],"tree":tree,"source":s})
 tm("source-parse")
 if not isinstance(x["targets"],list) or any(not isinstance(path,str) for path in x["targets"]):raise ValueError("invalid targets")
 targets=set(x["targets"])
 if len(targets)!=len(x["targets"]) or not targets.issubset(seen):raise ValueError("invalid targets")
 if type(x["complete"]) is not bool or x["complete"]!=(not targets or targets==seen):raise ValueError("invalid completeness")
 target_fs=[f for f in fs if f["path"] in targets]
 if not isinstance(x["references"],list):raise ValueError("invalid references")
 rs=[]; configured=[]; ids=set()
 for r in x["references"]:
  if not isinstance(r,dict) or set(r)!={"id","module","symbol","contract"}:raise ValueError("invalid reference")
  i=q(r["id"])
  if i in ids:raise ValueError("duplicate reference")
  ids.add(i)
  if r["contract"] is None:
   if not i.startswith("manifest:") or not n(r["module"]) or not n(r["symbol"]):raise ValueError("invalid inferred reference")
   rs.append(r)
  else:
   if not isinstance(r["contract"],dict) or r["contract"].get("id")!=i or r["module"] or r["symbol"]:raise ValueError("invalid configured reference")
   configured.append(r["contract"])
 if not isinstance(x["backends"],list):raise ValueError("invalid backends")
 bs=[]
 for b in x["backends"]:
  if not isinstance(b,dict) or set(b)!={"id","module","object"}:raise ValueError("invalid backend")
  i=q(b["id"])
  if i in ids or not n(b["module"]) or not isinstance(b["object"],str) or (b["object"] and not n(b["object"])):raise ValueError("invalid backend")
  ids.add(i);bs.append(b)
 if not isinstance(x["attributes"],list):raise ValueError("invalid attributes")
 ats=[]
 for a in x["attributes"]:
  if not isinstance(a,dict) or set(a)!={"id","module","callable","receiver","attribute","write"}:raise ValueError("invalid attribute")
  i=q(a["id"])
  if i in ids or not n(a["module"]) or not n(a["callable"]) or not n(a["attribute"]) or "." in a["attribute"]:raise ValueError("invalid attribute")
  _validate_external_receiver(a["receiver"]);_external_location(a["write"])
  ids.add(i);ats.append(a)
 mods={}
 for f in fs:
  if f["path"].endswith(".py") and f["module"]:mods.setdefault(f["module"],[]).append(f)
 tm("request-model")
 import vulture.core as vc
 vc.noqa.parse_noqa=lambda _:defaultdict(set)
 v=vc.Vulture()
 for f in fs:
  v.scan(f["source"],filename=f["path"]);o["covered"].append(f["path"])
 for i in {z.name for z in v.defined_imports}:
  w="whitelists/"+i+"_whitelist.py"
  try:d=pkgutil.get_data("vulture",w)
  except OSError:continue
  if d is not None:v.scan(d.decode("utf-8"),filename=w)
 tm("vulture-scan")
 try:
  type_modules=_vulture_type_modules(fs)
  tm("type-facts")
  typed=typed_dict_reads(type_modules,fields=True)
  tm("typed-dict")
  frameworks,resolved,problems=framework_members(type_modules,fs,target_fs,x["contracts"],x["complete"])
  tm("framework-contracts")
  o["resolved"].extend(resolved);o["problems"].extend(problems)
 except Exception as z:
  o["facts_error"]=str(z)[:S];raise
 keep={(f["path"],f["line"],f["line"],f["key"]) for f in typed}
 keep.update(frameworks)
 for f in fs:keep.update(ka(f))
 for r in rs:
  try:
   keep.update(rm(None,r["module"],r["symbol"].split("."),set()));o["resolved"].append(r["id"])
  except Exception as z:o["problems"].append({"id":r["id"],"message":str(z)[:S]})
 hooks=("build_wheel","build_sdist","get_requires_for_build_wheel","prepare_metadata_for_build_wheel","get_requires_for_build_sdist","build_editable","get_requires_for_build_editable","prepare_metadata_for_build_editable")
 for b in bs:
  try:
   z=mods.get(b["module"],[])
   if not z:raise ValueError("in-tree build backend module has no runtime .py definition")
   if len(z)!=1:raise ValueError("in-tree build backend module is ambiguous")
   f=z[0];body=f["tree"].body
   if b["object"]:
    parts=b["object"].split(".");d=ds(f,body,parts[0])
    if len(d)!=1 or d[0][0]!="d":raise ValueError("in-tree build backend object is stale or ambiguous")
    node=d[0][1];keep.add(c(f,node,parts[0]))
    for name in parts[1:]:
     if not isinstance(node,ast.ClassDef):raise ValueError("in-tree build backend object cannot be resolved statically")
     d=ds(f,node.body,name)
     if len(d)!=1 or d[0][0]!="d":raise ValueError("in-tree build backend object is stale or ambiguous")
     node=d[0][1];keep.add(c(f,node,name))
    if not isinstance(node,ast.ClassDef):raise ValueError("in-tree build backend object cannot expose hooks statically")
    body=node.body
   for name in hooks:
    d=ds(f,body,name)
    if len(d)>1:raise ValueError("in-tree build backend hook is ambiguous")
    if len(d)==1:
     if d[0][0]=="d":keep.add(c(f,d[0][1],name))
     else:keep.update(rm(None,b["module"],[name],set()))
   o["resolved"].append(b["id"])
  except Exception as z:o["problems"].append({"id":b["id"],"message":str(z)[:S]})
 for a in ats:
  try:
   keep.add(_external_write(a));o["resolved"].append(a["id"])
  except Exception as z:o["problems"].append({"id":a["id"],"message":str(z)[:S]})
 tm("static-contracts")
 from reachability import resolve_reachability
 evidence,problems=resolve_reachability(type_modules,configured,keep)
 tm("reachability")
 o["reachability"]=evidence;o["problems"].extend(problems)
 for item in evidence:
  o["resolved"].append(item["id"])
  for target in item["targets"]:
   for definition in target["definitions"]:keep.add((definition["path"],definition["line"],definition["end"],definition["name"]))
 for z in v.get_unused_code(min_confidence=60):
  a=(str(z.filename),z.first_lineno,z.last_lineno,z.name)
  if a[0] not in targets or a in keep:continue
  if not z.name or len(z.name)>S or not z.message or len(z.message)>S:raise ValueError("invalid Vulture diagnostic")
  o["diagnostics"].append({"path":a[0],"line":a[1],"end":a[2],"name":z.name,"kind":z.typ,"confidence":z.confidence,"message":z.message})
 tm("diagnostics")
except Exception as z:e(z)
o["timings"].append({"name":"adapter-total","duration_ns":time.perf_counter_ns()-started})
sys.stdout.write(json.dumps(o,sort_keys=True,separators=(",",":")))`

var pythonVultureProgram = pythonfacts.ParserSupportSource() + pythonfacts.TypeSupportSource() + pythonfacts.ReachabilitySupportSource() + pythonVultureTypeFacts + pythonVultureExternalAttributes + "\n" + pythonVultureAdapter

type pythonVultureFile struct {
	Path    string `json:"path"`
	Module  string `json:"module"`
	Package string `json:"package"`
}

type pythonVultureReference struct {
	Contract *pythonfacts.ReachabilityInput `json:"contract"`
	ID       string                         `json:"id"`
	Module   string                         `json:"module"`
	Symbol   string                         `json:"symbol"`
}

type pythonVultureBackend struct {
	ID     string `json:"id"`
	Module string `json:"module"`
	Object string `json:"object"`
}

type pythonVultureAttribute struct {
	ID        string                        `json:"id"`
	Module    string                        `json:"module"`
	Callable  string                        `json:"callable"`
	Receiver  policy.PythonExternalReceiver `json:"receiver"`
	Attribute string                        `json:"attribute"`
	Write     policy.PythonSourceLocation   `json:"write"`
}

type pythonVultureContract struct {
	policy.PythonContract
	ID string `json:"id"`
}

type pythonVultureRequest struct {
	Contracts   []pythonVultureContract  `json:"contracts"`
	Protocol    int                      `json:"protocol"`
	ToolVersion string                   `json:"tool_version"`
	Root        string                   `json:"root"`
	Files       []pythonVultureFile      `json:"files"`
	Targets     []string                 `json:"targets"`
	Complete    bool                     `json:"complete"`
	References  []pythonVultureReference `json:"references"`
	Backends    []pythonVultureBackend   `json:"backends"`
	Attributes  []pythonVultureAttribute `json:"attributes"`
}

type pythonVultureReferenceOrigin struct {
	Path    string
	Line    int
	Subject string
	Check   string
	Message string
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
	Reachability *[]pythonfacts.ReachabilityEvidence `json:"reachability"`
	Protocol     *int                                `json:"protocol"`
	ToolVersion  *string                             `json:"tool_version"`
	Covered      *[]string                           `json:"covered"`
	Diagnostics  *[]pythonVultureDiagnostic          `json:"diagnostics"`
	Resolved     *[]string                           `json:"resolved"`
	Problems     *[]pythonVultureProblem             `json:"problems"`
	FactsError   *string                             `json:"facts_error"`
	Error        *string                             `json:"error"`
	Timings      *[]pythonVultureTiming              `json:"timings"`
}

type pythonVultureResponse struct {
	Reachability []pythonfacts.ReachabilityEvidence
	Covered      []string
	Diagnostics  []pythonVultureDiagnostic
	Resolved     []string
	Problems     []pythonVultureProblem
	FactsError   string
	Error        string
	Timings      []pythonVultureTiming
}

func pythonVultureCommand(repo repository.Repository, project repository.PythonProject) (policy.Command, error) {
	return pythonVultureCommandForSources(repo, project, project.Files)
}

func pythonVultureCommandForSources(repo repository.Repository, project repository.PythonProject, targets []string) (policy.Command, error) {
	interpreter := repo.PythonTool()
	if !filepath.IsAbs(interpreter) {
		return policy.Command{}, fmt.Errorf("policy Python interpreter is not absolute")
	}
	files, targets, err := pythonVultureInputs(project, targets)
	if err != nil {
		return policy.Command{}, err
	}
	references, _ := pythonVultureReferences(repo, project)
	backends, _ := pythonVultureBackends(project)
	attributes, _ := pythonVultureAttributes(repo, project)
	request := pythonVultureRequest{
		Protocol: pythonVultureProtocolVersion, ToolVersion: pythonVultureVersion, Root: repo.Root,
		Files: files, Targets: targets, Complete: len(targets) == 0 || len(targets) == len(files),
		References: references, Backends: backends, Attributes: attributes, Contracts: pythonVultureContracts(repo, project),
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		return policy.Command{}, fmt.Errorf("encode Vulture request: %w", err)
	}
	if len(encoded) > pythonVultureInputMaximumBytes {
		return policy.Command{}, fmt.Errorf("vulture request exceeds %d bytes", pythonVultureInputMaximumBytes)
	}
	input, err := pythonfacts.ProgramInput(pythonVultureProgram, bytes.NewReader(append(encoded, '\n')))
	if err != nil {
		return policy.Command{}, err
	}
	stdin, err := io.ReadAll(input)
	if err != nil {
		return policy.Command{}, err
	}
	return policy.Command{
		Name:              "policy-vulture-dead-code-" + pythonQualityProjectName(project.Root),
		Provides:          []string{"dead-code"},
		Argv:              repo.PythonCommand(pythonfacts.ProgramBootstrap),
		Cwd:               ".",
		Modules:           pythonQualityModules(repo, project.Files),
		RunOn:             []string{"check", "gate"},
		TimeoutSeconds:    int(pythonQualityBudget.Seconds()),
		Managed:           true,
		SealedEnvironment: true,
		Stdin:             stdin,
	}, nil
}

func pythonVultureInputs(project repository.PythonProject, targets []string) ([]pythonVultureFile, []string, error) {
	files := make([]pythonVultureFile, 0, len(project.Files))
	for _, source := range project.Files {
		module, packageName := repository.PythonModuleName(project, source)
		files = append(files, pythonVultureFile{Path: source, Module: module, Package: packageName})
	}
	sort.Slice(files, func(left, right int) bool { return files[left].Path < files[right].Path })
	available := map[string]bool{}
	for _, file := range files {
		available[file.Path] = true
	}
	targets = append([]string{}, targets...)
	sort.Strings(targets)
	for index, target := range targets {
		if !available[target] || index > 0 && targets[index-1] == target {
			return nil, nil, fmt.Errorf("vulture target %q is not a unique project source", target)
		}
	}
	return files, targets, nil
}

func pythonVultureAttributes(repo repository.Repository, project repository.PythonProject) ([]pythonVultureAttribute, map[string]pythonVultureReferenceOrigin) {
	attributes := []pythonVultureAttribute{}
	origins := map[string]pythonVultureReferenceOrigin{}
	configPath := pythonVultureConfigPath(repo)
	for _, attribute := range repo.Config.Scope.PythonExternalAttributes {
		if attribute.Project != project.Manifest {
			continue
		}
		id := pythonVultureAttributeID(attribute)
		attributes = append(attributes, pythonVultureAttribute{
			ID: id, Module: attribute.Module, Callable: attribute.Callable, Receiver: attribute.Receiver,
			Attribute: attribute.Attribute, Write: attribute.Write,
		})
		origins[id] = pythonVultureReferenceOrigin{
			Path: configPath, Subject: id, Check: "policy.pythonExternalAttribute",
			Message: "Python external attribute cannot resolve exactly: ",
		}
	}
	sort.Slice(attributes, func(left, right int) bool { return attributes[left].ID < attributes[right].ID })
	return attributes, origins
}

func pythonVultureBackends(project repository.PythonProject) ([]pythonVultureBackend, map[string]pythonVultureReferenceOrigin) {
	if len(project.BackendPaths) == 0 || project.BuildBackend.Module == "" {
		return []pythonVultureBackend{}, map[string]pythonVultureReferenceOrigin{}
	}
	id := "manifest:" + project.Manifest + ":build-system.build-backend:" + project.BuildBackend.Module
	if project.BuildBackend.Object != "" {
		id += ":" + project.BuildBackend.Object
	}
	backend := pythonVultureBackend{ID: id, Module: project.BuildBackend.Module, Object: project.BuildBackend.Object}
	origin := pythonVultureReferenceOrigin{
		Path: project.Manifest, Line: project.BuildBackend.Line, Subject: id, Check: "policy.pythonBuildBackend",
		Message: "In-tree Python build backend cannot resolve exactly: ",
	}
	return []pythonVultureBackend{backend}, map[string]pythonVultureReferenceOrigin{id: origin}
}

func pythonVultureConfigReferenceID(reference policy.PythonDynamicReference) string {
	return repository.PythonReachabilityID(reference)
}

func pythonVultureManifestReferenceID(project repository.PythonProject, reference repository.PythonDynamicReference) string {
	return "manifest:" + project.Manifest + ":" + reference.Table + ":" + reference.Name + ":" + reference.Module + ":" + reference.Symbol
}

func pythonVultureAttributeID(attribute policy.PythonExternalAttribute) string {
	data, _ := json.Marshal(attribute)
	digest := sha256.Sum256(data)
	return "config:external-attribute:" + hex.EncodeToString(digest[:])
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
		if err != nil {
			continue
		}
		if filepath.Base(normalized) == "pyproject.toml" {
			manifests[normalized] = true
		}
		for _, reference := range repo.Config.Scope.PythonDynamicReferences {
			if reference.Registry != nil && normalized == reference.Registry.Path {
				manifests[reference.Project] = true
			}
		}
		if normalized == pythonVultureConfigPath(repo) {
			for _, contract := range repo.Config.Scope.PythonContracts {
				manifests[contract.Project] = true
			}
			for _, reference := range repo.Config.Scope.PythonDynamicReferences {
				manifests[reference.Project] = true
			}
			for _, attribute := range repo.Config.Scope.PythonExternalAttributes {
				manifests[attribute.Project] = true
			}
		}
	}
	return manifests
}

func pythonQualityDynamicReferenceInventoryFindings(repo repository.Repository, selectedManifests map[string]bool, message string) []policy.Finding {
	findings := make([]policy.Finding, 0, len(repo.Config.Scope.PythonDynamicReferences))
	configPath := pythonVultureConfigPath(repo)
	for _, reference := range repo.Config.Scope.PythonDynamicReferences {
		if !selectedManifests[reference.Project] {
			continue
		}
		findings = append(findings, policy.Finding{
			Check: "policy.pythonReachability", Path: configPath, Subject: pythonVultureConfigReferenceID(reference),
			Message: "Python dynamic reference cannot determine its contained project: " + message,
		})
	}
	return findings
}

func pythonQualityExternalAttributeInventoryFindings(repo repository.Repository, selectedManifests map[string]bool, message string) []policy.Finding {
	findings := make([]policy.Finding, 0, len(repo.Config.Scope.PythonExternalAttributes))
	configPath := pythonVultureConfigPath(repo)
	for _, attribute := range repo.Config.Scope.PythonExternalAttributes {
		if !selectedManifests[attribute.Project] {
			continue
		}
		findings = append(findings, policy.Finding{
			Check: "policy.pythonExternalAttribute", Path: configPath, Subject: pythonVultureAttributeID(attribute),
			Message: "Python external attribute cannot determine its contained project: " + message,
		})
	}
	return findings
}

func pythonQualityDynamicReferenceProjects(repo repository.Repository, projects map[string]repository.PythonProject, selectedByProject map[string][]string, selectedManifests map[string]bool, invalidProjects map[string]bool) ([]policy.Finding, map[string][]string) {
	findings, referenceOnly, unrunnable := pythonQualityConfigDynamicReferenceProjects(repo, projects, selectedByProject, selectedManifests, invalidProjects)
	return append(findings, pythonQualityInferredDynamicReferenceFindings(projects, selectedByProject, selectedManifests, referenceOnly, unrunnable, invalidProjects)...), referenceOnly
}

func pythonQualityConfigDynamicReferenceProjects(repo repository.Repository, projects map[string]repository.PythonProject, selectedByProject map[string][]string, selectedManifests, invalidProjects map[string]bool) ([]policy.Finding, map[string][]string, map[string]bool) {
	findings := []policy.Finding{}
	referenceOnly := map[string][]string{}
	unrunnable := map[string]bool{}
	for manifest, origins := range pythonQualityConfiguredReferenceOrigins(repo) {
		if !selectedManifests[manifest] && len(selectedByProject[manifest]) == 0 {
			continue
		}
		project, found := projects[manifest]
		problem := ""
		switch {
		case !found:
			problem = "names no current contained Python project"
		case invalidProjects[manifest], len(selectedByProject[manifest]) > 0:
			continue
		case !pythonQualityProjectHasRuntimeSource(project):
			problem = "names a project with no runtime .py definitions"
			unrunnable[manifest] = true
		default:
			referenceOnly[manifest] = nil
		}
		if problem != "" {
			for _, origin := range origins {
				findings = append(findings, policy.Finding{
					Check: origin.Check, Path: origin.Path, Subject: origin.Subject, Message: origin.Message + problem,
				})
			}
		}
	}
	return findings, referenceOnly, unrunnable
}

func pythonQualityConfiguredReferenceOrigins(repo repository.Repository) map[string][]pythonVultureReferenceOrigin {
	origins := map[string][]pythonVultureReferenceOrigin{}
	configPath := pythonVultureConfigPath(repo)
	for _, contract := range repo.Config.Scope.PythonContracts {
		origins[contract.Project] = append(origins[contract.Project], pythonContractOrigin(repo, contract))
	}
	for _, reference := range repo.Config.Scope.PythonDynamicReferences {
		origins[reference.Project] = append(origins[reference.Project], pythonVultureReferenceOrigin{
			Check: "policy.pythonReachability", Path: configPath, Subject: pythonVultureConfigReferenceID(reference),
			Message: "Python dynamic reference ",
		})
	}
	for _, attribute := range repo.Config.Scope.PythonExternalAttributes {
		origins[attribute.Project] = append(origins[attribute.Project], pythonVultureReferenceOrigin{
			Check: "policy.pythonExternalAttribute", Path: configPath, Subject: pythonVultureAttributeID(attribute),
			Message: "Python external attribute ",
		})
	}
	return origins
}

func pythonQualityInferredDynamicReferenceFindings(projects map[string]repository.PythonProject, selectedByProject map[string][]string, selectedManifests map[string]bool, referenceOnly map[string][]string, unrunnable map[string]bool, invalidProjects map[string]bool) []policy.Finding {
	findings := []policy.Finding{}
	for _, project := range projects {
		if invalidProjects[project.Manifest] || len(project.DynamicReferences) == 0 || !pythonQualityChecksInferredDynamicReferences(project.Manifest, selectedByProject, selectedManifests, referenceOnly, unrunnable) {
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
