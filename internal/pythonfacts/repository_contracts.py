import ast

from astroid_contracts import _AstroidContracts
from framework_contracts import _FrameworkResolver, _FrameworkVisitor
from type_facts import _reference_name

__all__ = ["framework_members"]


class _ContractVisitor(_FrameworkVisitor):
    def __init__(self, resolver, source, contract):
        super().__init__(resolver, source)
        self.contract = contract
        self.roots = {contract["target"]}

    def applies(self, reference):
        return self.contract["kind"] == "type" and self.resolver.derives(
            reference, self.roots
        )

    def callback(self, node):
        decorators = {
            self.qualified(value.func if isinstance(value, ast.Call) else value)
            for value in node.decorator_list
        }
        if self.contract["kind"] == "decorator":
            return any(self.decorated(value) for value in node.decorator_list)
        return self.applies(self.owner) and (
            node.name in self.contract.get("members", [])
            or bool(decorators & set(self.contract.get("decorators", [])))
        )

    def callback_parameters(self, node):
        return

    def decorated(self, node):
        target = node.func if isinstance(node, ast.Call) else node
        if self.qualified(target) != self.contract["target"]:
            return False
        keywords = self.contract.get("keywords", {})
        if not keywords:
            return True
        if not isinstance(node, ast.Call) or node.args:
            return False
        actual = {value.arg: value.value for value in node.keywords}
        return None not in actual and all(
            name in actual
            and isinstance(actual[name], ast.Constant)
            and actual[name].value is value
            for name, value in keywords.items()
        )

    def visit_ClassDef(self, node):
        if self.contract["kind"] == "decorator" and self.callback(node):
            self.keep(node, node.name)
        super().visit_ClassDef(node)

    def binding_value(self, node):
        if isinstance(node, (ast.List, ast.Tuple)):
            return bool(node.elts) and all(
                self.binding_value(value) for value in node.elts
            )
        target = node.func if isinstance(node, ast.Call) else node
        qualified = self.qualified(target)
        return qualified is not None and qualified.startswith(
            self.contract["target"] + "."
        )

    def connection_call(self, node, scope):
        return self.applies(
            self.resolver.reference(self.module, scope, _reference_name(node.func))
        )

    def parameters(self, node, scope):
        result = set()
        arguments = node.args.posonlyargs + node.args.args + node.args.kwonlyargs
        for argument in arguments:
            annotation = argument.annotation
            name = (
                annotation.value
                if isinstance(annotation, ast.Constant)
                and isinstance(annotation.value, str)
                else _reference_name(annotation)
            )
            if name and self.applies(self.resolver.reference(self.module, scope, name)):
                result.add((argument.arg,))
        decorators = {_reference_name(value) for value in node.decorator_list}
        if (
            arguments
            and self.applies(self.method_owner)
            and not decorators & {"staticmethod", "classmethod"}
        ):
            result.add((arguments[0].arg,))
        return result

    def visit_FunctionDef(self, node):
        previous = getattr(self, "method_owner", None)
        self.method_owner = self.owner
        super().visit_FunctionDef(node)
        self.method_owner = previous

    visit_AsyncFunctionDef = visit_FunctionDef

    def class_attribute(self, target, annotation=None):
        if not isinstance(target, ast.Name) or not self.applies(self.owner):
            return
        named = target.id in self.contract.get("attributes", [])
        annotated = annotation is not None and self.contract.get(
            "annotatedFields", False
        )
        if isinstance(annotation, ast.Subscript):
            annotated = annotated and self.qualified(annotation.value) not in {
                "typing.ClassVar",
                "typing_extensions.ClassVar",
            }
        if named or annotated:
            self.keep(target, target.id)

    def visit_Assign(self, node):
        for target in node.targets:
            self.module_binding(target, node.value)
            self.class_attribute(target)
        super().visit_Assign(node)

    def visit_AnnAssign(self, node):
        self.module_binding(node.target, node.value)
        self.class_attribute(node.target, node.annotation)
        super().visit_AnnAssign(node)

    def module_binding(self, target, value):
        if (
            self.contract["kind"] == "module-binding"
            and self.scope == "module"
            and isinstance(target, ast.Name)
            and target.id in self.contract.get("members", [])
            and self.binding_value(value)
        ):
            self.keep(target, target.id)

    def visit_Attribute(self, node):
        if (
            isinstance(node.ctx, ast.Store)
            and node.attr in self.contract.get("attributes", [])
            and self.connection(node.value)
        ):
            self.keep(node, node.attr)
        self.generic_visit(node)


def declared_members(resolver, sources, contracts):
    kept = set()
    problems = []
    resolved = []
    for contract in contracts:
        try:
            found = contract_members(resolver, sources, contract)
            if not found:
                raise ValueError("contract matches no source definitions")
            kept.update(found)
            resolved.append(contract["id"])
        except (ValueError, TypeError, *resolver.ancestry.errors) as error:
            problems.append({"id": contract["id"], "message": str(error)[:4096]})
    return kept, resolved, problems


def contract_members(resolver, sources, contract):
    if contract["kind"] == "entry-point":
        return resolver.ancestry.entry_point(
            contract["target"], contract.get("members", [])
        )
    kept = set()
    for source in sources:
        visitor = _ContractVisitor(resolver, source, contract)
        visitor.visit(source["tree"])
        kept.update(visitor.kept)
    return kept


_BUILTINS = (
    {"kind": "decorator", "target": "pytest.fixture", "keywords": {"autouse": True}},
    {"kind": "module-binding", "target": "pytest.mark", "members": ["pytestmark"]},
    {
        "kind": "type",
        "target": "socketserver.ThreadingMixIn",
        "attributes": ["daemon_threads"],
    },
    {
        "kind": "type",
        "target": "http.server.BaseHTTPRequestHandler",
        "attributes": ["close_connection"],
    },
)


def builtin_members(resolver, sources):
    kept = set()
    for contract in _BUILTINS:
        kept.update(contract_members(resolver, sources, contract))
    return kept


def framework_members(modules, sources, contracts):
    resolver = _FrameworkResolver(modules)
    kept = set()
    with _AstroidContracts(resolver, sources, contracts) as ancestry:
        resolver.ancestry = ancestry
        for source in sources:
            visitor = _FrameworkVisitor(resolver, source)
            visitor.visit(source["tree"])
            kept.update(visitor.kept)
        kept.update(builtin_members(resolver, sources))
        declared, resolved, problems = declared_members(resolver, sources, contracts)
        kept.update(declared)
    return kept, resolved, problems
