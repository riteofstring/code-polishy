import ast
import hashlib
import io
import keyword
import tokenize

MAX_TOKENS = 500000
MAX_AST_NODES = 200000
MAX_AST_DEPTH = 256


def _module_name(value):
    return (
        isinstance(value, str)
        and value
        and all(
            part.isidentifier() and not keyword.iskeyword(part)
            for part in value.split(".")
        )
    )


class _SourceVisitor(ast.NodeVisitor):
    def __init__(self):
        self.imports = []
        self.computed = []
        self.bindings = [
            {
                "__import__": "builtins.__import__",
                "open": "builtins.open",
                "next": "builtins.next",
            }
        ]
        self.callables = ["<module>"]

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
                {"module": item.name, "names": [], "line": node.lineno, "literal": True}
            )
            bound = item.asname or item.name.split(".")[0]
            self._shadow(bound)
            canonical = item.name if item.asname else item.name.split(".")[0]
            if canonical in {
                "importlib",
                "builtins",
                "json",
                "pathlib",
            } or item.name in {
                "importlib",
                "importlib.util",
                "importlib.metadata",
                "builtins",
                "json",
                "pathlib",
            }:
                self._bind(bound, canonical)

    def visit_ImportFrom(self, node):
        module = "." * node.level + (node.module or "")
        self.imports.append(
            {
                "module": module,
                "names": [item.name for item in node.names],
                "line": node.lineno,
                "literal": True,
            }
        )
        if any(item.name == "*" for item in node.names):
            for name in list(self.bindings[-1]):
                self._shadow(name)
            if node.level == 0 and node.module == "importlib":
                self._bind("import_module", "importlib.import_module")
            elif node.level == 0 and node.module == "builtins":
                self._bind("__import__", "builtins.__import__")
                self._bind("open", "builtins.open")
                self._bind("next", "builtins.next")
            return
        known = {
            ("importlib", "import_module"): "importlib.import_module",
            ("importlib", "metadata"): "importlib.metadata",
            ("importlib.metadata", "entry_points"): "importlib.metadata.entry_points",
            ("pathlib", "Path"): "pathlib.Path",
            ("builtins", "__import__"): "builtins.__import__",
            ("builtins", "open"): "builtins.open",
            ("builtins", "next"): "builtins.next",
        }
        for item in node.names:
            bound = item.asname or item.name
            self._shadow(bound)
            canonical = known.get((node.module, item.name)) if node.level == 0 else None
            if canonical:
                self._bind(bound, canonical)

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

    def _literal_collection(self, node):
        if isinstance(node, (ast.List, ast.Tuple, ast.Set)):
            nodes = node.elts
        elif isinstance(node, ast.Dict) and all(key is not None for key in node.keys):
            nodes = node.values
        else:
            return None
        values = set()
        for item in nodes:
            if not isinstance(item, ast.Constant) or not _module_name(item.value):
                return None
            values.add(item.value)
        return values if values else None

    def _literal_targets(self, node):
        if isinstance(node, ast.Constant) and _module_name(node.value):
            return {node.value}
        if isinstance(node, ast.IfExp):
            left = self._literal_targets(node.body)
            right = self._literal_targets(node.orelse)
            if left is not None and right is not None:
                return left | right
        if isinstance(node, ast.Subscript):
            return self._literal_collection(node.value)
        return None

    def _path_read(self, node):
        if (
            not isinstance(node, ast.Call)
            or self._callee(node.func) != "json.loads"
            or len(node.args) != 1
            or node.keywords
        ):
            return ""
        read = node.args[0]
        if (
            not isinstance(read, ast.Call)
            or read.args
            or any(keyword.arg != "encoding" for keyword in read.keywords)
        ):
            return ""
        if self._callee(read.func) != "pathlib.Path.read_text" and not (
            isinstance(read.func, ast.Attribute) and read.func.attr == "read_text"
        ):
            return ""
        path_call = read.func.value if isinstance(read.func, ast.Attribute) else None
        if (
            not isinstance(path_call, ast.Call)
            or self._callee(path_call.func) != "pathlib.Path"
            or len(path_call.args) != 1
            or path_call.keywords
        ):
            return ""
        path = path_call.args[0]
        if (
            not isinstance(path, ast.Constant)
            or not isinstance(path.value, str)
            or not path.value
        ):
            return ""
        for item in read.keywords:
            if (
                not isinstance(item.value, ast.Constant)
                or not isinstance(item.value.value, str)
                or item.value.value.lower().replace("_", "-") != "utf-8"
            ):
                return ""
        return path.value

    def _configuration_reference(self, node):
        slices = []
        current = node
        while isinstance(current, ast.Subscript):
            slices.append(current.slice)
            current = current.value
        path = self._path_read(current)
        if not path:
            return None
        slices.reverse()
        segments = []
        dynamic = False
        for index, item in enumerate(slices):
            if (
                isinstance(item, ast.Constant)
                and isinstance(item.value, (str, int))
                and not isinstance(item.value, bool)
            ):
                if dynamic:
                    return None
                segments.append(str(item.value))
                continue
            if dynamic or index != len(slices) - 1:
                return None
            dynamic = True
        if not segments:
            return None
        pointer = "/" + "/".join(
            segment.replace("~", "~0").replace("/", "~1") for segment in segments
        )
        return {"path": path, "jsonPointer": pointer}

    def _entry_point_group(self, node):
        if not isinstance(node, ast.Attribute) or node.attr != "module":
            return ""
        selection = node.value
        if isinstance(selection, ast.Subscript):
            call = selection.value
        elif (
            isinstance(selection, ast.Call)
            and self._callee(selection.func) == "builtins.next"
            and len(selection.args) == 1
            and not selection.keywords
        ):
            call = selection.args[0]
        else:
            return ""
        if (
            not isinstance(call, ast.Call)
            or self._callee(call.func) != "importlib.metadata.entry_points"
            or call.args
        ):
            return ""
        groups = [
            item.value.value
            for item in call.keywords
            if item.arg == "group"
            and isinstance(item.value, ast.Constant)
            and isinstance(item.value.value, str)
        ]
        if len(groups) != 1 or len(call.keywords) != 1:
            return ""
        return groups[0]

    def _evidence(self, node):
        targets = self._literal_targets(node)
        if targets is not None:
            return targets, set(), ""
        configuration = self._configuration_reference(node)
        if configuration is not None:
            return set(), {(configuration["path"], configuration["jsonPointer"])}, ""
        group = self._entry_point_group(node)
        if group:
            return set(), set(), group
        if isinstance(node, ast.IfExp):
            left = self._evidence(node.body)
            right = self._evidence(node.orelse)
            if (
                left is not None
                and right is not None
                and (not left[2] or not right[2] or left[2] == right[2])
            ):
                return left[0] | right[0], left[1] | right[1], left[2] or right[2]
        return None

    def visit_Call(self, node):
        callee = self._callee(node.func)
        if callee in {"importlib.import_module", "builtins.__import__"}:
            literal = (
                len(node.args) == 1
                and not node.keywords
                and isinstance(node.args[0], ast.Constant)
                and _module_name(node.args[0].value)
            )
            argument = (
                node.args[0].value
                if literal
                else ast.unparse(node.args[0])
                if node.args
                else ""
            )
            evidence = self._evidence(node.args[0]) if node.args else None
            call = {
                "callee": callee,
                "callable": self.callables[-1],
                "line": node.lineno,
                "column": node.col_offset + 1,
                "endLine": node.end_lineno or node.lineno,
                "endColumn": (node.end_col_offset or node.col_offset) + 1,
                "argument": argument,
                "shape": ast.dump(node, annotate_fields=True, include_attributes=False),
                "targets": sorted(evidence[0]) if evidence is not None else [],
                "configuration": [
                    {"path": path, "jsonPointer": pointer}
                    for path, pointer in sorted(evidence[1])
                ]
                if evidence is not None
                else [],
                "entryPointGroup": evidence[2] if evidence is not None else "",
                "evidenceError": ""
                if evidence is not None
                else "argument is outside the supported bounded computed-import shapes",
            }
            if literal:
                self.imports.append(
                    {
                        "module": argument,
                        "names": [],
                        "line": node.lineno,
                        "literal": True,
                    }
                )
            else:
                self.computed.append(call)
        self.generic_visit(node)

    def _function(self, node):
        self._visit_function_signature(node)
        name = self.callables[-1]
        qualified = node.name if name == "<module>" else name + "." + node.name
        bindings = dict(self.bindings[-1])
        bindings[node.name] = ""
        for argument in _function_arguments(node):
            bindings[argument.arg] = ""
        self._visit_callable_body(node.body, bindings, qualified)
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

    def _visit_callable_body(self, body, bindings, name):
        self.bindings.append(bindings)
        self.callables.append(name)
        for statement in body:
            self.visit(statement)
        self.callables.pop()
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
        name = self.callables[-1]
        qualified = node.name if name == "<module>" else name + "." + node.name
        self.bindings.append(dict(self.bindings[-1]))
        self.callables.append(qualified)
        for statement in node.body:
            self.visit(statement)
        self.callables.pop()
        self.bindings.pop()
        self._shadow(node.name)

    def visit_Lambda(self, node):
        for default in _function_defaults(node):
            self.visit(default)
        bindings = dict(self.bindings[-1])
        for argument in _function_arguments(node):
            bindings[argument.arg] = ""
        self.bindings.append(bindings)
        self.callables.append("<lambda>")
        self.visit(node.body)
        self.callables.pop()
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
    tokens = list(tokenize.generate_tokens(io.StringIO(source).readline))
    if len(tokens) > MAX_TOKENS:
        raise ValueError("Python source exceeds the token limit")
    tree = ast.parse(source, filename=path, type_comments=True, feature_version=(3, 12))
    count = 0
    stack = [(tree, 1)]
    while stack:
        node, depth = stack.pop()
        count += 1
        if count > MAX_AST_NODES:
            raise ValueError("Python source exceeds the AST node limit")
        if depth > MAX_AST_DEPTH:
            raise ValueError("Python source exceeds the AST depth limit")
        stack.extend((child, depth + 1) for child in ast.iter_child_nodes(node))
    visitor = _SourceVisitor()
    visitor.visit(tree)
    return {
        "path": path,
        "sha256": hashlib.sha256(source.encode("utf-8")).hexdigest(),
        "imports": visitor.imports,
        "computedImports": visitor.computed,
        "error": "",
    }
