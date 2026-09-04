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

const pythonVultureProtocolVersion = 4

const pythonVultureVersion = "2.16"

const pythonVultureInputMaximumBytes = 4 << 20

const pythonVultureAdapter = `import ast,json,os,pkgutil,re,sys
from collections import defaultdict
P=4
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
def un(x):
 while isinstance(x,ast.Subscript):x=x.value
 return x
def rd(mod,parts,seen):
 k=(mod,".".join(parts))
 if k in seen:return None
 seen=seen|{k};z=mods.get(mod,[])
 if len(z)!=1:return None
 f=z[0];d=ds(f,f["tree"].body,parts[0])
 if len(d)!=1:return None
 x=d[0]
 if x[0]=="i":
  node,a=x[1:];m=fm(f,node)
  if not m:return None
  return rd(m,a.name.split(".")+parts[1:],seen)
 node=x[1]
 if len(parts)==1:return (f,node) if isinstance(node,ast.ClassDef) else None
 if not isinstance(node,ast.ClassDef):return None
 d=ds(f,node.body,parts[1])
 if len(d)!=1 or d[0][0]!="d":return None
 node=d[0][1]
 if len(parts)==2:return (f,node) if isinstance(node,ast.ClassDef) else None
 return None
def rc(f,node,b):
 name=en(un(node),b)
 if not name:return None
 parts=name.split(".")
 if len(parts)==1 and f["module"]:
  z=rd(f["module"],parts,set())
  if z:return z
 for i in range(len(parts)-1,0,-1):
  mod=".".join(parts[:i])
  if mod in mods:
   z=rd(mod,parts[i:],set())
   if z:return z
 return None
def mi(f,node):
 return (f["path"],node.lineno,node.name)
def pa(f,node,b):
 name=node.target.id
 annotation=en(un(node.annotation),b)
 if annotation in ("typing.ClassVar","typing_extensions.ClassVar"):return False
 if not name.startswith("_"):return True
 return isinstance(node.value,ast.Call) and en(node.value.func,b) in ("pydantic.PrivateAttr","pydantic.v1.PrivateAttr")
def pk():
 roots={"pydantic.BaseModel","pydantic.v1.BaseModel","pydantic_settings.BaseSettings"}
 decorators={
  "pydantic.field_validator","pydantic.model_validator","pydantic.field_serializer",
  "pydantic.model_serializer","pydantic.computed_field","pydantic.validator",
  "pydantic.root_validator","pydantic.v1.validator","pydantic.v1.root_validator",
 }
 fields={"pydantic.Field","pydantic.PrivateAttr","pydantic.v1.Field","pydantic.v1.PrivateAttr"}
 classes=[]
 for f in fs:
  b=ib(f)
  for node in f["tree"].body:
   if isinstance(node,ast.ClassDef):classes.append((f,node,b))
 known=set();changed=True
 while changed:
  changed=False
  for f,node,b in classes:
   identity=mi(f,node)
   if identity in known:continue
   for base in node.bases:
    name=en(un(base),b);target=rc(f,base,b)
    if name in roots or (target and mi(*target) in known):
     known.add(identity);changed=True;break
 keep=set()
 for f,node,b in classes:
  if mi(f,node) not in known:continue
  for item in node.body:
   if isinstance(item,ast.AnnAssign) and isinstance(item.target,ast.Name) and (item.target.id=="model_config" or pa(f,item,b)):keep.add(c(f,item,item.target.id))
   elif isinstance(item,ast.Assign):
    names=[z for target in item.targets for z in ts(target)]
    call=en(item.value.func,b) if isinstance(item.value,ast.Call) else ""
    for name in names:
     if name.id=="model_config" or call in fields:keep.add(c(f,item,name.id))
   if isinstance(item,(ast.FunctionDef,ast.AsyncFunctionDef)):
    for decorator in item.decorator_list:
     if en(decorator.func if isinstance(decorator,ast.Call) else decorator,b) in decorators:
      keep.add(c(f,item,item.name));break
 return keep
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
def xa(a):
 f,node=fc(a["module"],a["callable"].split("."));b=ib(f);args=node.args
 parameters=args.posonlyargs+args.args+args.kwonlyargs+([args.vararg] if args.vararg else [])+([args.kwarg] if args.kwarg else [])
 parameters=[x for x in parameters if x.arg==a["receiver"]]
 if len(parameters)!=1 or parameters[0].annotation is None:raise ValueError("write receiver has no exact annotated parameter")
 if en(un(parameters[0].annotation),b)!=a["consumer_type"]:raise ValueError("write receiver annotation does not match consumer type")
 if any(a["consumer_type"]==m or a["consumer_type"].startswith(m+".") for m in mods):raise ValueError("consumer type is not external")
 writes=[x for x in wn(node) if isinstance(x,ast.Attribute) and isinstance(x.ctx,ast.Store) and x.lineno==a["line"] and x.attr==a["attribute"] and isinstance(x.value,ast.Name) and x.value.id==a["receiver"]]
 if len(writes)!=1:raise ValueError("external attribute write is stale or ambiguous")
 return c(f,writes[0],a["attribute"])
try:
 b=sys.stdin.buffer.read(M+1)
 if len(b)>M:raise ValueError("input exceeds limit")
 x=json.loads(b)
 if not isinstance(x,dict) or set(x)!={"protocol","tool_version","root","files","references","backends","attributes"}:raise ValueError("invalid request")
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
  if not isinstance(a,dict) or set(a)!={"id","module","callable","receiver","attribute","line","consumer_type"}:raise ValueError("invalid attribute")
  i=q(a["id"])
  if i in ids or not n(a["module"]) or not n(a["callable"]) or not n(a["receiver"]) or "." in a["receiver"] or not n(a["attribute"]) or "." in a["attribute"] or type(a["line"]) is not int or a["line"]<1 or not n(a["consumer_type"]) or "." not in a["consumer_type"]:raise ValueError("invalid attribute")
  ids.add(i);ats.append(a)
 mods={}
 for f in fs:
  if f["path"].endswith(".py") and f["module"]:mods.setdefault(f["module"],[]).append(f)
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
 keep=pk()
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
   keep.add(xa(a));o["resolved"].append(a["id"])
  except Exception as z:o["problems"].append({"id":a["id"],"message":str(z)[:S]})
 for z in v.get_unused_code(min_confidence=60):
  a=(str(z.filename),z.first_lineno,z.last_lineno,z.name)
  if a in keep:continue
  if not z.name or len(z.name)>S or not z.message or len(z.message)>S:raise ValueError("invalid Vulture diagnostic")
  o["diagnostics"].append({"path":a[0],"line":a[1],"end":a[2],"name":z.name,"kind":z.typ,"confidence":z.confidence,"message":z.message})
except Exception as z:e(z)
sys.stdout.write(json.dumps(o,sort_keys=True,separators=(",",":")))`

var pythonVultureProgram = "exec(" + strconv.Quote(pythonVultureAdapter) + ")"

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

type pythonVultureBackend struct {
	ID     string `json:"id"`
	Module string `json:"module"`
	Object string `json:"object"`
}

type pythonVultureAttribute struct {
	ID           string `json:"id"`
	Module       string `json:"module"`
	Callable     string `json:"callable"`
	Receiver     string `json:"receiver"`
	Attribute    string `json:"attribute"`
	Line         int    `json:"line"`
	ConsumerType string `json:"consumer_type"`
}

type pythonVultureRequest struct {
	Protocol    int                      `json:"protocol"`
	ToolVersion string                   `json:"tool_version"`
	Root        string                   `json:"root"`
	Files       []pythonVultureFile      `json:"files"`
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
	backends, _ := pythonVultureBackends(project)
	attributes, _ := pythonVultureAttributes(repo, project)
	request := pythonVultureRequest{
		Protocol: pythonVultureProtocolVersion, ToolVersion: pythonVultureVersion, Root: repo.Root,
		Files: files, References: references, Backends: backends, Attributes: attributes,
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
		Argv:              repo.PythonCommand(pythonVultureProgram),
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
		origins[id] = pythonVultureReferenceOrigin{
			Path: configPath, Subject: id, Check: "policy.pythonDynamicReference",
			Message: "Python dynamic reference cannot resolve exactly: ",
		}
	}
	for _, reference := range project.DynamicReferences {
		id := pythonVultureManifestReferenceID(project, reference)
		references = append(references, pythonVultureReference{ID: id, Module: reference.Module, Symbol: reference.Symbol})
		origins[id] = pythonVultureReferenceOrigin{
			Path: project.Manifest, Line: reference.Line, Subject: id, Check: "policy.pythonDynamicReference",
			Message: "Python dynamic reference cannot resolve exactly: ",
		}
	}
	sort.Slice(references, func(left, right int) bool { return references[left].ID < references[right].ID })
	return references, origins
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
			Attribute: attribute.Attribute, Line: attribute.Line, ConsumerType: attribute.ConsumerType,
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
	return "config:" + reference.Project + ":" + reference.Module + ":" + reference.Symbol
}

func pythonVultureManifestReferenceID(project repository.PythonProject, reference repository.PythonDynamicReference) string {
	return "manifest:" + project.Manifest + ":" + reference.Table + ":" + reference.Name + ":" + reference.Module + ":" + reference.Symbol
}

func pythonVultureAttributeID(attribute policy.PythonExternalAttribute) string {
	return fmt.Sprintf("config:%s:%s:%s:%s.%s:%d:%s", attribute.Project, attribute.Module, attribute.Callable,
		attribute.Receiver, attribute.Attribute, attribute.Line, attribute.ConsumerType)
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

func pythonQualityExternalAttributeInventoryFindings(repo repository.Repository, message string) []policy.Finding {
	findings := make([]policy.Finding, 0, len(repo.Config.Scope.PythonExternalAttributes))
	configPath := pythonVultureConfigPath(repo)
	for _, attribute := range repo.Config.Scope.PythonExternalAttributes {
		findings = append(findings, policy.Finding{
			Check: "policy.pythonExternalAttribute", Path: configPath, Subject: pythonVultureAttributeID(attribute),
			Message: "Python external attribute cannot determine its contained project: " + message,
		})
	}
	return findings
}

func pythonQualityDynamicReferenceProjects(repo repository.Repository, projects map[string]repository.PythonProject, selectedByProject map[string][]string, selectedManifests map[string]bool, invalidProjects map[string]bool) ([]policy.Finding, map[string][]string) {
	findings, referenceOnly, unrunnable := pythonQualityConfigDynamicReferenceProjects(repo, projects, selectedByProject, invalidProjects)
	return append(findings, pythonQualityInferredDynamicReferenceFindings(projects, selectedByProject, selectedManifests, referenceOnly, unrunnable, invalidProjects)...), referenceOnly
}

func pythonQualityConfigDynamicReferenceProjects(repo repository.Repository, projects map[string]repository.PythonProject, selectedByProject map[string][]string, invalidProjects map[string]bool) ([]policy.Finding, map[string][]string, map[string]bool) {
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
		if invalidProjects[project.Manifest] {
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
	for _, attribute := range repo.Config.Scope.PythonExternalAttributes {
		project, found := projects[attribute.Project]
		if !found {
			findings = append(findings, policy.Finding{
				Check: "policy.pythonExternalAttribute", Path: configPath, Subject: pythonVultureAttributeID(attribute),
				Message: "Python external attribute names no current contained Python project",
			})
			continue
		}
		if invalidProjects[project.Manifest] {
			continue
		}
		if _, selected := selectedByProject[project.Manifest]; selected {
			continue
		}
		if !pythonQualityProjectHasRuntimeSource(project) {
			findings = append(findings, policy.Finding{
				Check: "policy.pythonExternalAttribute", Path: configPath, Subject: pythonVultureAttributeID(attribute),
				Message: "Python external attribute names a project with no runtime .py definitions",
			})
			unrunnable[project.Manifest] = true
			continue
		}
		referenceOnly[project.Manifest] = nil
	}
	return findings, referenceOnly, unrunnable
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

func pythonVultureFindings(repo repository.Repository, project repository.PythonProject, data []byte) []policy.Finding {
	response, err := parsePythonVultureResponse(data)
	if err != nil {
		return pythonVultureCoverage(project.Files, "the policy-owned Vulture output cannot be used: "+err.Error())
	}
	if response.Error != "" {
		return pythonVultureCoverage(project.Files, "the policy-owned Vulture analysis failed: "+response.Error)
	}
	references, origins := pythonVultureReferences(repo, project)
	backends, backendOrigins := pythonVultureBackends(project)
	attributes, attributeOrigins := pythonVultureAttributes(repo, project)
	for id, origin := range backendOrigins {
		origins[id] = origin
	}
	for id, origin := range attributeOrigins {
		origins[id] = origin
	}
	if err := validatePythonVultureResponse(project.Files, references, backends, attributes, response); err != nil {
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

func validatePythonVultureResponse(files []string, references []pythonVultureReference, backends []pythonVultureBackend, attributes []pythonVultureAttribute, response pythonVultureResponse) error {
	if err := validatePythonVultureCovered(files, response.Covered); err != nil {
		return err
	}
	if err := validatePythonVultureReferences(references, backends, attributes, response.Resolved, response.Problems); err != nil {
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

func validatePythonVultureReferences(references []pythonVultureReference, backends []pythonVultureBackend, attributes []pythonVultureAttribute, resolved []string, problems []pythonVultureProblem) error {
	remaining, err := pythonVultureRequestedReferences(references, backends, attributes)
	if err != nil {
		return err
	}
	if err := pythonVultureConsumeResolvedReferences(remaining, resolved); err != nil {
		return err
	}
	if err := pythonVultureConsumeReferenceProblems(remaining, problems); err != nil {
		return err
	}
	if len(remaining) != 0 {
		return fmt.Errorf("reference result omits requested references")
	}
	return nil
}

func pythonVultureRequestedReferences(references []pythonVultureReference, backends []pythonVultureBackend, attributes []pythonVultureAttribute) (map[string]bool, error) {
	requested := map[string]bool{}
	for _, reference := range references {
		if requested[reference.ID] {
			return nil, fmt.Errorf("policy dynamic references are not unique")
		}
		requested[reference.ID] = true
	}
	for _, backend := range backends {
		if requested[backend.ID] {
			return nil, fmt.Errorf("vulture references are not unique")
		}
		requested[backend.ID] = true
	}
	for _, attribute := range attributes {
		if requested[attribute.ID] {
			return nil, fmt.Errorf("vulture references are not unique")
		}
		requested[attribute.ID] = true
	}
	return requested, nil
}

func pythonVultureConsumeResolvedReferences(remaining map[string]bool, resolved []string) error {
	for _, id := range resolved {
		if !remaining[id] {
			return fmt.Errorf("resolved references are not an exact request")
		}
		delete(remaining, id)
	}
	return nil
}

func pythonVultureConsumeReferenceProblems(remaining map[string]bool, problems []pythonVultureProblem) error {
	for _, problem := range problems {
		if !remaining[problem.ID] || strings.TrimSpace(problem.Message) == "" || len(problem.Message) > pythonStructuredMessageMaximumBytes {
			return fmt.Errorf("problem references are not an exact request")
		}
		delete(remaining, problem.ID)
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
			Check: origin.Check, Path: origin.Path, Line: origin.Line, Subject: origin.Subject,
			Message: origin.Message + problem.Message,
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
