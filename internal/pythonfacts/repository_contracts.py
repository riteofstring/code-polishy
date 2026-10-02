import ast

from astroid_contracts import _AstroidContracts
from framework_contracts import _FrameworkResolver, _FrameworkVisitor
from type_facts import _reference_name

__all__ = ["framework_members"]

_PARAMETER_INPUT = object()


class _ContractVisitor(_FrameworkVisitor):
    def __init__(self, resolver, source, contract, writes):
        super().__init__(resolver, source, writes)
        self.contract = contract
        self.roots = {contract["target"]}
        self.parameter_members = set()

    def parameter_input(self, path):
        return len(path) == 2 and path[1] is _PARAMETER_INPUT

    def persistent_connection(self, path):
        return super().persistent_connection(path) or self.parameter_input(path)

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
        names = self.contract.get("callbackParameters", {}).get(node.name, [])
        if not names or not self.callback(node):
            return
        arguments = node.args.posonlyargs + node.args.args + node.args.kwonlyargs
        arguments += [
            argument
            for argument in (node.args.vararg, node.args.kwarg)
            if argument is not None
        ]
        missing = set(names) - {argument.arg for argument in arguments}
        if missing:
            raise ValueError(
                f"callback {node.name} has no declared parameters: "
                + ", ".join(sorted(missing))
            )
        for argument in arguments:
            if argument.arg in names:
                self.keep(argument, argument.arg)
        self.parameter_members.add(node.name)

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
        return result | {(*path, _PARAMETER_INPUT) for path in result}

    def visit_FunctionDef(self, node):
        previous = getattr(self, "method_owner", None)
        self.method_owner = self.owner
        super().visit_FunctionDef(node)
        self.method_owner = previous

    visit_AsyncFunctionDef = visit_FunctionDef

    def loop_entry(self, node, initial):
        entry = {
            path
            for path in initial
            if self.parameter_input(path) or (*path, _PARAMETER_INPUT) in initial
        }
        if isinstance(node, (ast.For, ast.AsyncFor)):
            previous = self.connections
            self.connections = entry
            self.bind(node.target)
            entry = self.connections.copy()
            self.connections = previous
        return entry

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


def declared_members(resolver, sources, contracts, writes, require_match):
    kept = set()
    problems = []
    resolved = []
    for contract in contracts:
        try:
            found = contract_members(resolver, sources, contract, writes)
            if not found and require_match:
                raise ValueError("contract matches no source definitions")
            kept.update(found)
            if require_match:
                resolved.append(contract["id"])
        except (ValueError, TypeError, *resolver.ancestry.errors) as error:
            if require_match:
                problems.append({"id": contract["id"], "message": str(error)[:4096]})
    return kept, resolved, problems


def contract_members(resolver, sources, contract, writes):
    if contract["kind"] == "entry-point":
        return resolver.ancestry.entry_point(
            contract["target"], contract.get("members", [])
        )
    kept = set()
    parameter_members = set()
    for source in sources:
        visitor = _ContractVisitor(resolver, source, contract, writes[source["path"]])
        visitor.visit(source["tree"])
        kept.update(visitor.kept)
        parameter_members.update(visitor.parameter_members)
    missing = set(contract.get("callbackParameters", {})) - parameter_members
    if missing:
        raise ValueError(
            "callback parameter members match no source definitions: "
            + ", ".join(sorted(missing))
        )
    return kept


def framework_members(modules, sources, targets, contracts, require_contracts):
    resolver = _FrameworkResolver(modules)
    kept = set()
    with _AstroidContracts(resolver, sources, contracts) as ancestry:
        resolver.ancestry = ancestry
        writes = {}
        for source in targets:
            visitor = _FrameworkVisitor(resolver, source)
            visitor.visit(source["tree"])
            kept.update(visitor.kept)
            writes[source["path"]] = visitor.writes
        contract_sources = sources if require_contracts else targets
        if contracts:
            for source in contract_sources:
                if source["path"] not in writes:
                    writes[source["path"]] = _FrameworkVisitor(resolver, source).writes
        declared, resolved, problems = declared_members(
            resolver, contract_sources, contracts, writes, require_contracts
        )
        kept.update(declared)
    return kept, resolved, problems
