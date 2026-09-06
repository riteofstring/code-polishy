import ast

from type_facts import _reference_name


def _path(node):
    name = _reference_name(node)
    return tuple(name.split(".")) if name else ()


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
            if len(path) == 1
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

    def loop(self, node):
        initial = self.connections.copy()
        body = self.block(node.body, set())
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

    def visit_ListComp(self, node):
        self.connections.clear()

    visit_SetComp = visit_ListComp
    visit_DictComp = visit_ListComp
    visit_GeneratorExp = visit_ListComp
