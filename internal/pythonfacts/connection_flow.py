import ast

from type_facts import _reference_name


def _path(node):
    name = _reference_name(node)
    return tuple(name.split(".")) if name else ()


def _local_target_names(node):
    if isinstance(node, ast.Name):
        return {node.id}
    if isinstance(node, ast.Starred):
        return _local_target_names(node.value)
    if isinstance(node, (ast.Tuple, ast.List)):
        return {name for item in node.elts for name in _local_target_names(item)}
    return set()


class _ConnectionFlow(ast.NodeVisitor):
    def __init__(self, connection_factory):
        self.connections = set()
        self.mutable_names = set()
        self.scope = "module"
        self.connection_factory = connection_factory

    def connection(self, node):
        if isinstance(node, ast.Call):
            return self.connection_factory(node, self.scope)
        return _path(node) in self.connections

    def persistent_connection(self, path):
        return len(path) == 1

    def bind(self, target, connected=False):
        if isinstance(target, (ast.Tuple, ast.List)):
            for item in target.elts:
                self.bind(item)
            return
        path = _path(target)
        self.connections = {
            known
            for known in self.connections
            if known[: len(path)] != path
            and not (len(path) > 1 and path[-1] in known[1:])
        }
        if connected and path:
            self.connections.add(path)

    def visit_Assign(self, node):
        connected = self.connection(node.value)
        self.visit(node.value)
        for target in node.targets:
            self.visit(target)
            self.bind(target, connected)

    def visit_AnnAssign(self, node):
        if node.value is None:
            return
        connected = self.connection(node.value)
        if node.value is not None:
            self.visit(node.value)
        self.visit(node.target)
        self.bind(node.target, connected)

    def visit_AugAssign(self, node):
        self.generic_visit(node)
        self.bind(node.target)

    def visit_NamedExpr(self, node):
        connected = self.connection(node.value)
        self.visit(node.value)
        self.bind(node.target, connected)

    def visit_Delete(self, node):
        for target in node.targets:
            self.bind(target)

    def visit_Call(self, node):
        self.generic_visit(node)
        self.connections = {
            path
            for path in self.connections
            if self.persistent_connection(path)
            and path[0] not in self.mutable_names
            and self.scope != "module"
        }

    def block(self, statements, initial):
        self.connections = initial.copy()
        for statement in statements:
            self.visit(statement)
        return self.connections.copy()

    def visit_If(self, node):
        self.visit(node.test)
        initial = self.connections.copy()
        left = self.block(node.body, initial)
        right = self.block(node.orelse, initial)
        self.connections = left & right

    def visit_IfExp(self, node):
        self.visit(node.test)
        initial = self.connections.copy()
        left = self.block([node.body], initial)
        right = self.block([node.orelse], initial)
        self.connections = left & right

    def visit_BoolOp(self, node):
        possible = []
        for value in node.values:
            self.visit(value)
            possible.append(self.connections.copy())
        self.connections = set.intersection(*possible)

    def visit_Try(self, node):
        normal = self.block(node.body, self.connections)
        normal = self.block(node.orelse, normal)
        exits = [normal]
        for handler in node.handlers:
            exits.append(self.block(handler.body, set()))
        merged = set.intersection(*exits)
        self.connections = (
            self.block(node.finalbody, set()) if node.finalbody else merged
        )

    def visit_TryStar(self, node):
        self.visit_Try(node)

    def visit_For(self, node):
        self.visit(node.iter)
        self.loop(node)

    visit_AsyncFor = visit_For

    def visit_While(self, node):
        self.visit(node.test)
        self.loop(node)

    def loop_entry(self, node, initial):
        return set()

    def loop(self, node):
        initial = self.connections.copy()
        body = self.block(node.body, self.loop_entry(node, initial))
        self.connections = self.block(node.orelse, initial & body)

    def visit_With(self, node):
        initial = self.connections.copy()
        nonsuppressing = True
        for item in node.items:
            connected = self.connection(item.context_expr)
            nonsuppressing = nonsuppressing and connected
            self.visit(item.context_expr)
            if item.optional_vars is not None:
                self.bind(item.optional_vars, connected)
        normal = self.block(node.body, self.connections)
        self.connections = normal if nonsuppressing else initial & normal

    def visit_AsyncWith(self, node):
        self.block(node.body, set())
        self.connections.clear()

    def visit_Import(self, node):
        for alias in node.names:
            self.bind(ast.Name(id=alias.asname or alias.name.split(".")[0]))

    def visit_ImportFrom(self, node):
        for alias in node.names:
            if alias.name == "*":
                self.connections.clear()
            else:
                self.bind(ast.Name(id=alias.asname or alias.name))

    def visit_Match(self, node):
        self.visit(node.subject)
        initial = self.connections.copy()
        exits = [initial]
        for case in node.cases:
            exits.append(self.block(case.body, set()))
        self.connections = set.intersection(*exits)

    def visit_Lambda(self, node):
        return

    def comprehension(self, node, outputs):
        self.visit(node.generators[0].iter)
        initial = self.connections.copy()
        previous_scope = self.scope
        self.scope = f"{self.scope}/comprehension:{node.lineno}:{node.col_offset + 1}"
        local_names = set()
        for index, generator in enumerate(node.generators):
            if index:
                self.visit(generator.iter)
            local_names.update(_local_target_names(generator.target))
            self.visit(generator.target)
            self.bind(generator.target)
            for condition in generator.ifs:
                self.visit(condition)
        for output in outputs:
            self.visit(output)
        completed = self.connections
        self.scope = previous_scope
        self.connections = {
            path for path in initial if path in completed or path[0] in local_names
        }

    def visit_ListComp(self, node):
        self.comprehension(node, [node.elt])

    def visit_SetComp(self, node):
        self.comprehension(node, [node.elt])

    def visit_DictComp(self, node):
        self.comprehension(node, [node.key, node.value])

    def visit_GeneratorExp(self, node):
        self.comprehension(node, [node.elt])
