import ast
from collections import Counter

from connection_flow import _ConnectionFlow
from type_facts import _reference_name
from type_resolver import _Resolver


class _FrameworkResolver(_Resolver):
    def __init__(self, modules):
        super().__init__(modules)
        self.ancestry = None

    def reference(self, module, scope, name, seen=frozenset()):
        try:
            return super().reference(module, scope, name, seen)
        except ValueError:
            return None

    def qualified(self, module, scope, node):
        name = _reference_name(node)
        binding = self.binding(module, scope, name.partition(".")[0])
        if binding is None:
            return None
        if binding["scope"] == scope and binding["site"]["line"] > node.lineno:
            return None
        reference = self.reference(module, scope, name)
        return reference if isinstance(reference, str) else None

    def derives(self, reference, roots):
        return self.ancestry is not None and self.ancestry.derives(reference, roots)


class _FrameworkVisitor(_ConnectionFlow):
    def __init__(self, resolver, source, writes=None):
        super().__init__(self.connection_call)
        self.resolver = resolver
        self.module = resolver.files[source["path"]]
        self.scope = "module"
        self.owner = None
        self.kept = set()
        if writes is None:
            writes = Counter(
                (
                    node.lineno,
                    node.attr if isinstance(node, ast.Attribute) else node.id,
                )
                for node in ast.walk(source["tree"])
                if isinstance(node, (ast.Attribute, ast.Name))
                and isinstance(node.ctx, ast.Store)
            )
        self.writes = writes

    def keep(self, node, name):
        decorators = getattr(node, "decorator_list", [])
        start = decorators[0].lineno if decorators else node.lineno
        end = (
            node.end_lineno
            if isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef, ast.ClassDef))
            else start
        )
        if self.writes[(start, name)] <= 1:
            self.kept.add((self.module["path"], start, end, name))

    def qualified(self, node):
        return self.resolver.qualified(self.module, self.scope, node)

    def visit_ClassDef(self, node):
        previous = self.scope, self.owner, self.connections, self.mutable_names
        self.owner = self.resolver.reference(self.module, self.scope, node.name)
        self.scope = f"{self.scope}/class:{node.lineno}:{node.col_offset + 1}"
        self.connections = self.block(node.body, set())
        self.scope, self.owner, self.connections, self.mutable_names = previous
        self.bind(ast.Name(id=node.name))
        self.connections = {path for path in self.connections if len(path) == 1}

    def visit_FunctionDef(self, node):
        if self.callback(node) or self.pytest_autouse(node):
            self.keep(node, node.name)
            self.callback_parameters(node)
        previous = self.scope, self.owner, self.connections, self.mutable_names
        self.scope = f"{self.scope}/function:{node.lineno}:{node.col_offset + 1}"
        self.connections = self.parameters(node, previous[0])
        self.owner = None
        self.mutable_names = {
            name
            for value in ast.walk(node)
            if isinstance(value, (ast.Nonlocal, ast.Global))
            for name in value.names
        }
        for statement in node.body:
            self.visit(statement)
        self.scope, self.owner, self.connections, self.mutable_names = previous
        self.bind(ast.Name(id=node.name))
        self.connections = {path for path in self.connections if len(path) == 1}

    visit_AsyncFunctionDef = visit_FunctionDef

    def pytest_autouse(self, node):
        for value in node.decorator_list:
            target = value.func if isinstance(value, ast.Call) else value
            if self.qualified(target) != "pytest.fixture":
                continue
            if not isinstance(value, ast.Call) or value.args:
                continue
            keywords = {keyword.arg: keyword.value for keyword in value.keywords}
            autouse = keywords.get("autouse")
            if isinstance(autouse, ast.Constant) and autouse.value is True:
                return True
        return False

    def pytest_mark_value(self, node):
        if isinstance(node, (ast.List, ast.Tuple)):
            return bool(node.elts) and all(
                self.pytest_mark_value(value) for value in node.elts
            )
        target = node.func if isinstance(node, ast.Call) else node
        qualified = self.qualified(target)
        return qualified is not None and qualified.startswith("pytest.mark.")

    def class_binding(self, target, value=None):
        if not isinstance(target, ast.Name):
            return
        if (
            self.scope == "module"
            and target.id == "pytestmark"
            and self.pytest_mark_value(value)
        ):
            self.keep(target, target.id)
        if target.id == "daemon_threads" and self.resolver.derives(
            self.owner, {"socketserver.ThreadingMixIn"}
        ):
            self.keep(target, target.id)

    def visit_Assign(self, node):
        for target in node.targets:
            self.class_binding(target, node.value)
        super().visit_Assign(node)

    def visit_AnnAssign(self, node):
        self.class_binding(node.target, node.value)
        super().visit_AnnAssign(node)

    def connection_call(self, node, scope):
        qualified = self.resolver.qualified(self.module, scope, node.func)
        if qualified not in {"sqlite3.connect", "sqlite3.Connection"}:
            return False
        if any(isinstance(argument, ast.Starred) for argument in node.args):
            return False
        if any(keyword.arg is None for keyword in node.keywords):
            return False
        factories = [
            keyword.value for keyword in node.keywords if keyword.arg == "factory"
        ]
        if len(node.args) > 5:
            factories.append(node.args[5])
        return all(
            self.resolver.qualified(self.module, scope, value) == "sqlite3.Connection"
            for value in factories
        )

    def parameters(self, node, scope):
        result = set()
        arguments = node.args.posonlyargs + node.args.args + node.args.kwonlyargs
        for argument in arguments:
            annotation = argument.annotation
            if isinstance(annotation, ast.Constant) and isinstance(
                annotation.value, str
            ):
                qualified = self.resolver.reference(
                    self.module, scope, annotation.value
                )
            else:
                qualified = self.resolver.qualified(self.module, scope, annotation)
            if qualified == "sqlite3.Connection":
                result.add((argument.arg,))
        if arguments and self.resolver.derives(
            self.owner, {"http.server.BaseHTTPRequestHandler"}
        ):
            result.add((arguments[0].arg,))
        return result

    def callback_parameters(self, node):
        arguments = node.args.posonlyargs + node.args.args
        buffered = node.name == "readinto" and self.resolver.derives(
            self.owner, {"io.RawIOBase"}
        )
        logging = node.name == "log_message" and self.resolver.derives(
            self.owner, {"http.server.BaseHTTPRequestHandler"}
        )
        if (buffered or logging) and len(arguments) > 1:
            self.keep(arguments[1], arguments[1].arg)
        if logging and node.args.vararg is not None:
            self.keep(node.args.vararg, node.args.vararg.arg)

    def callback(self, node):
        if self.resolver.derives(self.owner, {"io.RawIOBase"}):
            return node.name in {"readable", "readinto"}
        if self.resolver.derives(self.owner, {"http.server.BaseHTTPRequestHandler"}):
            return node.name == "log_message" or (
                node.name.startswith("do_") and len(node.name) > 3
            )
        return False

    def visit_Attribute(self, node):
        if (
            isinstance(node.ctx, ast.Store)
            and (node.attr == "row_factory" or node.attr == "close_connection")
            and self.connection(node.value)
        ):
            self.keep(node, node.attr)
        self.generic_visit(node)
