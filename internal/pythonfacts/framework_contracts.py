import ast
from collections import Counter

from type_facts import _reference_name
from type_resolver import MAX_DEPTH, _Resolver

__all__ = ["framework_members"]


class _FrameworkResolver(_Resolver):
    def qualified(self, module, scope, node):
        name = _reference_name(node)
        binding = self.binding(module, scope, name.partition(".")[0])
        if binding is None:
            return None
        if binding["scope"] == scope and binding["site"]["line"] > node.lineno:
            return None
        reference = self.reference(module, scope, name)
        return reference if isinstance(reference, str) else None

    def state_machine(self, reference, seen=frozenset()):
        if isinstance(reference, str):
            return reference == "hypothesis.stateful.RuleBasedStateMachine"
        if reference is None:
            return False
        path, binding = reference
        identity = path, binding["scope"], binding["name"]
        if identity in seen or len(seen) >= MAX_DEPTH:
            return False
        if binding["kind"] != "class":
            return False
        return any(
            base["kind"] == "name"
            and self.state_machine(
                self.reference(self.files[path], binding["scope"], base["name"]),
                seen | {identity},
            )
            for base in binding["bases"]
        )


class _FrameworkVisitor(ast.NodeVisitor):
    def __init__(self, resolver, source):
        self.resolver = resolver
        self.module = resolver.files[source["path"]]
        self.scope = "module"
        self.owner = None
        self.kept = set()
        self.context_bindings = {}
        self.writes = Counter(
            (node.lineno, node.attr if isinstance(node, ast.Attribute) else node.id)
            for node in ast.walk(source["tree"])
            if isinstance(node, (ast.Attribute, ast.Name))
            and isinstance(node.ctx, ast.Store)
        )
        self.nodes = {
            (node.lineno, node.col_offset + 1): node
            for node in ast.walk(source["tree"])
            if isinstance(node, ast.Call)
        }

    def keep(self, node, name):
        decorators = getattr(node, "decorator_list", [])
        start = decorators[0].lineno if decorators else node.lineno
        end = (
            node.end_lineno
            if isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef))
            else start
        )
        if self.writes[(start, name)] <= 1:
            self.kept.add((self.module["path"], start, end, name))

    def qualified(self, node):
        return self.resolver.qualified(self.module, self.scope, node)

    def visit_ClassDef(self, node):
        previous = self.scope, self.owner
        self.owner = self.resolver.reference(self.module, self.scope, node.name)
        self.scope = f"{self.scope}/class:{node.lineno}:{node.col_offset + 1}"
        for statement in node.body:
            self.visit(statement)
        self.scope, self.owner = previous

    def visit_FunctionDef(self, node):
        if self.resolver.state_machine(self.owner) and node.name == "teardown":
            self.keep(node, node.name)
        if any(self.autouse(decorator) for decorator in node.decorator_list):
            self.keep(node, node.name)
        previous = self.scope, self.owner
        self.scope = f"{self.scope}/function:{node.lineno}:{node.col_offset + 1}"
        self.owner = None
        for statement in node.body:
            self.visit(statement)
        self.scope, self.owner = previous

    visit_AsyncFunctionDef = visit_FunctionDef

    def autouse(self, node):
        if self.module["scopes"][self.scope]["kind"] == "function":
            return False
        if (
            not isinstance(node, ast.Call)
            or node.args
            or any(keyword.arg is None for keyword in node.keywords)
            or self.qualified(node.func) != "pytest.fixture"
        ):
            return False
        return any(
            keyword.arg == "autouse"
            and isinstance(keyword.value, ast.Constant)
            and keyword.value.value is True
            for keyword in node.keywords
        )

    def marks(self, node):
        if isinstance(node, (ast.List, ast.Tuple)):
            return bool(node.elts) and all(self.marks(value) for value in node.elts)
        target = node.func if isinstance(node, ast.Call) else node
        qualified = self.qualified(target)
        return qualified is not None and qualified.startswith("pytest.mark.")

    def visit_Assign(self, node):
        if self.scope == "module" and self.marks(node.value):
            for target in node.targets:
                if isinstance(target, ast.Name) and target.id == "pytestmark":
                    self.keep(target, target.id)
        self.generic_visit(node)

    def visit_AnnAssign(self, node):
        if (
            self.scope == "module"
            and isinstance(node.target, ast.Name)
            and node.target.id == "pytestmark"
            and self.marks(node.value)
        ):
            self.keep(node.target, node.target.id)
        self.generic_visit(node)

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

    def connection(self, node, scope=None, seen=frozenset()):
        scope = self.scope if scope is None else scope
        if isinstance(node, ast.Call):
            return self.connection_call(node, scope)
        if not isinstance(node, ast.Name):
            return False
        binding = self.resolver.binding(self.module, scope, node.id)
        if binding is None:
            binding = self.context_bindings.get((scope, node.id))
        if binding is None or (binding["site"]["line"], binding["site"]["column"]) >= (
            node.lineno,
            node.col_offset + 1,
        ):
            return False
        identity = binding["scope"], binding["name"]
        if identity in seen or len(seen) >= MAX_DEPTH:
            return False
        return self.connection_binding(binding, seen | {identity})

    def connection_binding(self, binding, seen):
        if binding["kind"] == "parameter":
            annotation = binding["annotation"]
            return (
                annotation["kind"] in {"name", "string"}
                and self.resolver.reference(
                    self.module, binding["annotationScope"], annotation["name"]
                )
                == "sqlite3.Connection"
            )
        site = binding["valueSite"] or binding["site"]
        value = self.nodes.get((site["line"], site["column"]))
        if value is None and binding["value"]["kind"] == "name":
            value = ast.Name(
                id=binding["value"]["name"],
                lineno=site["line"],
                col_offset=site["column"] - 1,
            )
        return self.connection(value, binding["scope"], seen)

    def visit_With(self, node):
        previous = self.context_bindings.copy()
        for item in node.items:
            self.context_binding(item)
        for statement in node.body:
            self.visit(statement)
        self.context_bindings = previous

    def context_binding(self, item):
        target = item.optional_vars
        if not isinstance(target, ast.Name):
            return
        bindings = self.module["bindings"].get((self.scope, target.id), [])
        if len(bindings) != 1:
            return
        binding = bindings[0]
        if binding["site"] != {"line": target.lineno, "column": target.col_offset + 1}:
            return
        self.context_bindings[(self.scope, target.id)] = binding
        self.nodes[(target.lineno, target.col_offset + 1)] = item.context_expr

    def visit_Attribute(self, node):
        if (
            isinstance(node.ctx, ast.Store)
            and node.attr == "row_factory"
            and self.connection(node.value)
        ):
            self.keep(node, node.attr)
        self.generic_visit(node)


def framework_members(modules, sources):
    resolver = _FrameworkResolver(modules)
    kept = set()
    for source in sources:
        visitor = _FrameworkVisitor(resolver, source)
        visitor.visit(source["tree"])
        kept.update(visitor.kept)
    return kept
