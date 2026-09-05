import ast
import hashlib

from source_parser import parse_source
from type_facts import type_facts


class _SourceVisitor(ast.NodeVisitor):
    def __init__(self, path):
        self.path = path
        self.imports = []
        self.type_only = 0
        self.bindings = [{}]

    def _import_kind(self):
        if self.type_only or self.path.endswith(".pyi"):
            return "type-only"
        if self.path.endswith("/__init__.py") or self.path == "__init__.py":
            return "re-export"
        return "runtime"

    def _bind(self, name, value):
        self.bindings[-1][name] = value

    def _shadow(self, name):
        self.bindings[-1][name] = ""

    def _visit_target(self, target):
        if isinstance(target, ast.Name):
            self._shadow(target.id)
        elif isinstance(target, ast.Starred):
            self._visit_target(target.value)
        elif isinstance(target, (ast.Tuple, ast.List)):
            for item in target.elts:
                self._visit_target(item)
        else:
            self.visit(target)

    def visit_Import(self, node):
        for item in node.names:
            self.imports.append(
                {
                    "module": item.name,
                    "names": [],
                    "line": node.lineno,
                    "column": node.col_offset + 1,
                    "kind": self._import_kind(),
                    "literal": True,
                }
            )
            bound = item.asname or item.name.split(".")[0]
            self._shadow(bound)
            canonical = item.name if item.asname else item.name.split(".")[0]
            self._bind(bound, canonical)

    def visit_ImportFrom(self, node):
        module = "." * node.level + (node.module or "")
        self.imports.append(
            {
                "module": module,
                "names": [item.name for item in node.names],
                "line": node.lineno,
                "column": node.col_offset + 1,
                "kind": self._import_kind(),
                "literal": True,
            }
        )
        if any(item.name == "*" for item in node.names):
            for name in list(self.bindings[-1]):
                self._shadow(name)
            return
        known = {("typing", "TYPE_CHECKING"): "typing.TYPE_CHECKING"}
        for item in node.names:
            bound = item.asname or item.name
            self._shadow(bound)
            canonical = known.get((node.module, item.name)) if node.level == 0 else None
            if canonical:
                self._bind(bound, canonical)

    def visit_If(self, node):
        self.visit(node.test)
        if self._callee(node.test) == "typing.TYPE_CHECKING":
            self.type_only += 1
            for statement in node.body:
                self.visit(statement)
            self.type_only -= 1
            for statement in node.orelse:
                self.visit(statement)
            return
        for statement in node.body:
            self.visit(statement)
        for statement in node.orelse:
            self.visit(statement)

    def visit_Assign(self, node):
        self.visit(node.value)
        for target in node.targets:
            self._visit_target(target)

    def visit_AnnAssign(self, node):
        self.visit(node.annotation)
        if node.value is not None:
            self.visit(node.value)
        self._visit_target(node.target)

    def visit_NamedExpr(self, node):
        self.visit(node.value)
        self._visit_target(node.target)

    def visit_AugAssign(self, node):
        if not isinstance(node.target, ast.Name):
            self.visit(node.target)
        self.visit(node.value)
        self._visit_target(node.target)

    def visit_Delete(self, node):
        for target in node.targets:
            self._visit_target(target)

    def _visit_for(self, node):
        self.visit(node.iter)
        self._visit_target(node.target)
        for statement in node.body:
            self.visit(statement)
        for statement in node.orelse:
            self.visit(statement)

    def visit_For(self, node):
        self._visit_for(node)

    def visit_AsyncFor(self, node):
        self._visit_for(node)

    def _visit_with(self, node):
        for item in node.items:
            self.visit(item.context_expr)
            if item.optional_vars is not None:
                self._visit_target(item.optional_vars)
        for statement in node.body:
            self.visit(statement)

    def visit_With(self, node):
        self._visit_with(node)

    def visit_AsyncWith(self, node):
        self._visit_with(node)

    def visit_ExceptHandler(self, node):
        if node.type is not None:
            self.visit(node.type)
        if node.name is not None:
            self._shadow(node.name)
        for statement in node.body:
            self.visit(statement)

    def _callee(self, node):
        if isinstance(node, ast.Name):
            for bindings in reversed(self.bindings):
                if node.id in bindings:
                    return bindings[node.id]
            return ""
        if isinstance(node, ast.Attribute):
            base = self._callee(node.value)
            if base:
                return base + "." + node.attr
        return ""

    def _function(self, node):
        self._visit_function_signature(node)
        bindings = dict(self.bindings[-1])
        bindings[node.name] = ""
        for argument in _function_arguments(node):
            bindings[argument.arg] = ""
        self._visit_callable_body(node.body, bindings)
        self._shadow(node.name)

    def _visit_function_signature(self, node):
        for decorator in node.decorator_list:
            self.visit(decorator)
        for argument in _function_arguments(node):
            if argument.annotation is not None:
                self.visit(argument.annotation)
        for default in _function_defaults(node):
            self.visit(default)
        if node.returns is not None:
            self.visit(node.returns)
        for type_parameter in getattr(node, "type_params", []):
            self.visit(type_parameter)

    def _visit_callable_body(self, body, bindings):
        self.bindings.append(bindings)
        for statement in body:
            self.visit(statement)
        self.bindings.pop()

    def visit_FunctionDef(self, node):
        self._function(node)

    def visit_AsyncFunctionDef(self, node):
        self._function(node)

    def visit_ClassDef(self, node):
        for decorator in node.decorator_list:
            self.visit(decorator)
        for base in node.bases:
            self.visit(base)
        for keyword_argument in node.keywords:
            self.visit(keyword_argument.value)
        for type_parameter in getattr(node, "type_params", []):
            self.visit(type_parameter)
        self.bindings.append(dict(self.bindings[-1]))
        for statement in node.body:
            self.visit(statement)
        self.bindings.pop()
        self._shadow(node.name)

    def visit_Lambda(self, node):
        for default in _function_defaults(node):
            self.visit(default)
        bindings = dict(self.bindings[-1])
        for argument in _function_arguments(node):
            bindings[argument.arg] = ""
        self.bindings.append(bindings)
        self.visit(node.body)
        self.bindings.pop()

    def _visit_comprehension(self, node, outputs):
        self.bindings.append(dict(self.bindings[-1]))
        for generator in node.generators:
            self.visit(generator.iter)
            self._visit_target(generator.target)
            for condition in generator.ifs:
                self.visit(condition)
        for output in outputs:
            self.visit(output)
        self.bindings.pop()

    def visit_ListComp(self, node):
        self._visit_comprehension(node, [node.elt])

    def visit_SetComp(self, node):
        self._visit_comprehension(node, [node.elt])

    def visit_GeneratorExp(self, node):
        self._visit_comprehension(node, [node.elt])

    def visit_DictComp(self, node):
        self._visit_comprehension(node, [node.key, node.value])


def _function_arguments(node):
    arguments = (
        list(node.args.posonlyargs) + list(node.args.args) + list(node.args.kwonlyargs)
    )
    if node.args.vararg is not None:
        arguments.append(node.args.vararg)
    if node.args.kwarg is not None:
        arguments.append(node.args.kwarg)
    return arguments


def _function_defaults(node):
    return list(node.args.defaults) + [
        item for item in node.args.kw_defaults if item is not None
    ]


def _source(path, source):
    tree = parse_source(path, source)
    visitor = _SourceVisitor(path)
    visitor.visit(tree)
    return {
        "path": path,
        "sha256": hashlib.sha256(source.encode("utf-8")).hexdigest(),
        "imports": visitor.imports,
        "typeFacts": type_facts(tree),
        "error": "",
    }
