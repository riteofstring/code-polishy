import ast

from object_predicates import object_predicate


def _reference_name(node):
    if isinstance(node, ast.Name):
        return node.id
    if isinstance(node, ast.Attribute):
        prefix = _reference_name(node.value)
        return prefix + "." + node.attr if prefix else ""
    return ""


def _expression(node):
    reference = _reference_name(node)
    if reference:
        return {"kind": "name", "name": reference, "args": []}
    if isinstance(node, ast.List):
        return {
            "kind": "list",
            "name": "",
            "args": [_expression(value) for value in node.elts],
        }
    if isinstance(node, ast.Call):
        return {"kind": "call", "name": _reference_name(node.func), "args": []}
    if isinstance(node, ast.Subscript):
        arguments = (
            node.slice.elts if isinstance(node.slice, ast.Tuple) else [node.slice]
        )
        return {
            "kind": "subscript",
            "name": _reference_name(node.value),
            "args": [_expression(value) for value in arguments],
        }
    if isinstance(node, ast.BinOp) and isinstance(node.op, ast.BitOr):
        return {
            "kind": "union",
            "name": "",
            "args": [_expression(node.left), _expression(node.right)],
        }
    if isinstance(node, ast.Constant):
        return _constant_expression(node.value)
    return {"kind": "unknown", "name": "", "args": []}


def _constant_expression(value):
    if value is Ellipsis:
        return {"kind": "ellipsis", "name": "", "args": []}
    if isinstance(value, str):
        return {"kind": "string", "name": value, "args": []}
    if value is None or isinstance(value, (int, float, bool)):
        return {"kind": "literal", "name": repr(value), "args": []}
    return {"kind": "unknown", "name": "", "args": []}


def _position(node):
    return {"line": node.lineno, "column": node.col_offset + 1}


def _end_position(node):
    return {"line": node.end_lineno, "column": node.end_col_offset + 1}


def _call_argument(node, depth=0):
    if depth >= 64:
        raise ValueError("Python consumer expression exceeds its depth boundary")
    text = ast.unparse(node)
    if len(text.encode("utf-8")) > 65536:
        raise ValueError("Python call argument exceeds its compact text boundary")
    kind, value, children = "expression", "", []
    if isinstance(node, ast.Constant) and isinstance(node.value, str):
        kind, value = "string", node.value
    elif _reference_name(node):
        kind, value = "name", _reference_name(node)
    elif isinstance(node, ast.Call):
        kind, value = "call", _reference_name(node.func)
    elif isinstance(node, ast.Starred):
        kind = "starred"
    else:
        kind, value, children = _consumer_selection(node, depth)
    return {
        "kind": kind,
        "value": value,
        "text": text,
        "site": _position(node),
        "endSite": _end_position(node),
        "children": children,
    }


def _consumer_selection(node, depth):
    if isinstance(node, ast.IfExp):
        return (
            "choice",
            "",
            [_call_argument(value, depth + 1) for value in (node.body, node.orelse)],
        )
    if isinstance(node, (ast.List, ast.Tuple, ast.Set)):
        return (
            "collection",
            "",
            [_call_argument(value, depth + 1) for value in node.elts],
        )
    if isinstance(node, ast.Dict) and all(key is not None for key in node.keys):
        return (
            "collection",
            "",
            [_call_argument(value, depth + 1) for value in node.values],
        )
    if isinstance(node, ast.Subscript):
        return (
            "subscript",
            "",
            [
                _call_argument(node.value, depth + 1),
                _call_argument(node.slice, depth + 1),
            ],
        )
    if isinstance(node, ast.Attribute):
        return "attribute", node.attr, [_call_argument(node.value, depth + 1)]
    if isinstance(node, ast.Constant) and type(node.value) is int:
        return "integer", str(node.value), []
    return "expression", "", []


def _field(name, node, annotation):
    return {"name": name, "site": _position(node), "type": _expression(annotation)}


def _total_keywords(node):
    return len(node.keywords) <= 1 and all(
        keyword.arg == "total"
        and isinstance(keyword.value, ast.Constant)
        and isinstance(keyword.value.value, bool)
        for keyword in node.keywords
    )


def _factory(node):
    result = {"name": "", "fields": [], "valid": False}
    if not isinstance(node, ast.Call) or len(node.args) != 2:
        return result
    name, fields = node.args
    if not isinstance(name, ast.Constant) or not isinstance(name.value, str):
        return result
    result["name"] = name.value
    if not isinstance(fields, ast.Dict) or not _total_keywords(node):
        return result
    for key, value in zip(fields.keys, fields.values, strict=True):
        if not isinstance(key, ast.Constant) or not isinstance(key.value, str):
            return result
        result["fields"].append(_field(key.value, key, value))
    result["valid"] = True
    return result


def _class_fields(node):
    fields = []
    valid = _total_keywords(node) and not node.type_params and not node.decorator_list
    for statement in node.body:
        if isinstance(statement, ast.AnnAssign) and isinstance(
            statement.target, ast.Name
        ):
            fields.append(
                _field(statement.target.id, statement.target, statement.annotation)
            )
            valid = valid and statement.value is None
        elif isinstance(statement, ast.Pass):
            continue
        elif isinstance(statement, ast.Expr) and isinstance(
            statement.value, ast.Constant
        ):
            valid = valid and isinstance(statement.value.value, str)
        else:
            valid = False
    return fields, valid


def _rejecting_call(node):
    if (
        not isinstance(node, ast.If)
        or node.orelse
        or len(node.body) != 1
        or not isinstance(node.body[0], ast.Raise)
        or not isinstance(node.test, ast.UnaryOp)
        or not isinstance(node.test.op, ast.Not)
        or not isinstance(node.test.operand, ast.Call)
        or any(
            isinstance(value, (ast.Yield, ast.YieldFrom))
            for value in ast.walk(node.body[0])
        )
    ):
        return None
    return node.test.operand


def _validator_site(node):
    if isinstance(node, ast.AsyncFunctionDef):
        return None
    arguments = node.args.posonlyargs + node.args.args
    if (
        len(arguments) != 1
        or node.args.kwonlyargs
        or node.args.vararg
        or node.args.kwarg
        or node.args.defaults
        or node.decorator_list
        or len(node.body) not in {1, 2}
    ):
        return None
    call = _rejecting_call(node.body[0])
    if call is None:
        return None
    if len(node.body) == 2 and not _validator_return(node.body[1], arguments[0].arg):
        return None
    return _position(call)


def _validator_return(node, parameter):
    return isinstance(node, ast.Return) and (
        node.value is None
        or (isinstance(node.value, ast.Name) and node.value.id == parameter)
    )


class _TypeVisitor(ast.NodeVisitor):
    def __init__(self):
        self.scopes = [{"id": "module", "parent": "", "kind": "module"}]
        self.by_scope = {"module": self.scopes[0]}
        self.scope = "module"
        self.bindings = []
        self.reads = []
        self.calls = []
        self.conditional = 0
        self.statement = -1
        self.required_calls = set()
        self.direct_calls = set()
        self.type_guards = []
        self.flow = []
        self.activation_site = None

    def _binding(self, name, node, kind="unknown", **values):
        decorators = getattr(node, "decorator_list", ())
        binding = {
            "scope": self.scope,
            "name": name,
            "kind": kind,
            "site": _position(node),
            "endSite": _end_position(node),
            "activationSite": self.activation_site or _end_position(node),
            "definitionLine": decorators[0].lineno if decorators else node.lineno,
            "valueSite": None,
            "valueEndSite": None,
            "conditional": self.conditional > 0,
            "flow": list(self.flow),
            "statement": self.statement,
            "decorators": [_expression(value) for value in decorators],
            "runtimeClass": False,
            "validator": None,
            "objectPredicate": None,
            "reference": "",
            "annotationScope": self.scope,
            "annotation": _expression(None),
            "value": _expression(None),
            "bases": [],
            "fields": [],
            "classValid": False,
            "factory": {"name": "", "fields": [], "valid": False},
        }
        binding.update(values)
        binding["activationSite"] = binding["valueEndSite"] or binding["activationSite"]
        self.bindings.append(binding)
        return binding

    def _enter_scope(self, node, kind):
        parent = self.scope
        if kind in {"function", "comprehension"}:
            while self.by_scope[parent]["kind"] == "class":
                parent = self.by_scope[parent]["parent"]
        identifier = f"{self.scope}/{kind}:{node.lineno}:{node.col_offset + 1}"
        scope = {"id": identifier, "parent": parent, "kind": kind}
        self.scopes.append(scope)
        self.by_scope[identifier] = scope
        previous = self.scope, self.conditional
        self.scope = identifier
        self.conditional = 0
        return previous

    def visit_Import(self, node):
        for alias in node.names:
            name = alias.asname or alias.name.split(".")[0]
            reference = alias.name if alias.asname else alias.name.split(".")[0]
            self._binding(name, node, "import", reference=reference)

    def visit_ImportFrom(self, node):
        prefix = "." * node.level + (node.module or "")
        for alias in node.names:
            reference = prefix + ("" if prefix.endswith(".") else ".") + alias.name
            self._binding(
                alias.asname or alias.name, node, "import", reference=reference
            )

    def visit_Assign(self, node):
        self.visit(node.value)
        previous = self.activation_site
        self.activation_site = _end_position(node.value)
        for target in node.targets:
            if isinstance(target, ast.Name) and len(node.targets) == 1:
                self._binding(
                    target.id,
                    target,
                    "alias",
                    value=_expression(node.value),
                    valueSite=_position(node.value),
                    valueEndSite=_end_position(node.value),
                    factory=_factory(node.value),
                )
            else:
                self.visit(target)
        self.activation_site = previous

    def visit_AugAssign(self, node):
        self.visit(node.value)
        previous = self.activation_site
        self.activation_site = _end_position(node.value)
        self.visit(node.target)
        self.activation_site = previous

    def visit_AnnAssign(self, node):
        self.visit(node.annotation)
        if node.value is not None:
            self.visit(node.value)
        if isinstance(node.target, ast.Name):
            self._binding(
                node.target.id,
                node.target,
                "annotated",
                activationSite=_end_position(node),
                annotation=_expression(node.annotation),
                value=_expression(node.value),
                valueSite=_position(node.value) if node.value is not None else None,
                valueEndSite=_end_position(node.value)
                if node.value is not None
                else None,
            )
        else:
            self.visit(node.target)

    def visit_Name(self, node):
        if isinstance(node.ctx, (ast.Store, ast.Del)):
            self._binding(node.id, node)

    def visit_NamedExpr(self, node):
        self.visit(node.value)
        previous = self.scope
        while self.by_scope[self.scope]["kind"] == "comprehension":
            self.scope = self.by_scope[self.scope]["parent"]
        self._binding(
            node.target.id, node.target, activationSite=_end_position(node.value)
        )
        self.scope = previous

    def visit_Subscript(self, node):
        if (
            isinstance(node.ctx, ast.Load)
            and isinstance(node.slice, ast.Constant)
            and isinstance(node.slice.value, str)
        ):
            self.reads.append(
                {
                    "scope": self.scope,
                    "site": _position(node),
                    "receiver": _expression(node.value),
                    "key": node.slice.value,
                }
            )
        self.generic_visit(node)

    def visit_Call(self, node):
        shape = ast.dump(node, annotate_fields=True, include_attributes=False)
        self.calls.append(
            {
                "scope": self.scope,
                "site": _position(node),
                "endSite": _end_position(node),
                "callee": _call_argument(node.func),
                "arguments": [_call_argument(argument) for argument in node.args],
                "keywords": [
                    {"name": keyword.arg, "value": _call_argument(keyword.value)}
                    for keyword in node.keywords
                ],
                "conditional": self.conditional > 0,
                "flow": list(self.flow),
                "statement": self.statement,
                "guard": id(node) in self.required_calls,
                "direct": id(node) in self.direct_calls,
                "shape": shape if len(shape.encode("utf-8")) <= 16384 else "",
                "typeGuards": list(self.type_guards),
            }
        )
        self.generic_visit(node)

    def visit_ClassDef(self, node):
        fields, valid = _class_fields(node)
        self._binding(
            node.name,
            node,
            "class",
            bases=[_expression(base) for base in node.bases],
            fields=fields,
            classValid=valid,
            runtimeClass=not node.keywords and not node.type_params,
        )
        for expression in (
            node.bases
            + node.decorator_list
            + [keyword.value for keyword in node.keywords]
        ):
            self.visit(expression)
        previous = self._enter_scope(node, "class")
        for statement in node.body:
            self.visit(statement)
        self.scope, self.conditional = previous

    def _function(self, node):
        annotation_scope = self.scope
        arguments = node.args.posonlyargs + node.args.args + node.args.kwonlyargs
        expressions = list(node.args.defaults) + [
            value for value in node.args.kw_defaults if value is not None
        ]
        expressions += [
            argument.annotation
            for argument in arguments
            if argument.annotation is not None
        ]
        if not isinstance(node, ast.Lambda):
            self._binding(
                node.name,
                node,
                "function",
                validator=_validator_site(node),
                objectPredicate=object_predicate(node),
            )
            expressions += node.decorator_list
            if node.returns is not None:
                expressions.append(node.returns)
        for expression in expressions:
            self.visit(expression)
        previous = self._enter_scope(node, "function")
        for argument in arguments:
            self._binding(
                argument.arg,
                argument,
                "parameter",
                annotation=_expression(argument.annotation),
                annotationScope=annotation_scope,
            )
        for argument in (node.args.vararg, node.args.kwarg):
            if argument is not None:
                self._binding(argument.arg, argument)
        if isinstance(node, ast.Lambda):
            self.visit(node.body)
        else:
            self._statements(node.body)
        self.scope, self.conditional = previous

    def _statements(self, statements):
        previous = self.statement
        for index, statement in enumerate(statements):
            self.statement = index
            if isinstance(statement, (ast.Expr, ast.Assign, ast.AnnAssign, ast.Return)):
                value = statement.value
                if isinstance(value, ast.Call):
                    self.direct_calls.add(id(value))
            self.visit(statement)
        self.statement = previous

    def visit_FunctionDef(self, node):
        self._function(node)

    def visit_AsyncFunctionDef(self, node):
        self._function(node)

    def visit_Lambda(self, node):
        self._function(node)

    def _conditional(self, node):
        self.conditional += 1
        self.generic_visit(node)
        self.conditional -= 1

    def visit_If(self, node):
        call = _rejecting_call(node)
        if call is not None and self.conditional == 0:
            self.required_calls.add(id(call))
        self.conditional += 1
        self.visit(node.test)
        name = _reference_name(node.test)
        if name:
            self.type_guards.append(
                {"scope": self.scope, "name": name, "site": _position(node.test)}
            )
        self._branch(node, "if", node.body)
        if name:
            self.type_guards.pop()
        self._branch(node, "else", node.orelse)
        self.conditional -= 1

    def _branch(self, node, kind, statements):
        self.flow.append(f"{kind}:{node.lineno}:{node.col_offset + 1}")
        for statement in statements:
            self.visit(statement)
        self.flow.pop()

    def _for(self, node):
        self.visit(node.iter)
        self.conditional += 1
        self.flow.append(f"for:{node.lineno}:{node.col_offset + 1}")
        previous = self.activation_site
        self.activation_site = _end_position(node.iter)
        self.visit(node.target)
        self.activation_site = previous
        for statement in node.body:
            self.visit(statement)
        self.flow.pop()
        self._branch(node, "for-else", node.orelse)
        self.conditional -= 1

    def visit_For(self, node):
        self._for(node)

    def visit_AsyncFor(self, node):
        self._for(node)

    def visit_While(self, node):
        self._conditional(node)

    def visit_Try(self, node):
        self._conditional(node)

    def visit_TryStar(self, node):
        self._conditional(node)

    def visit_With(self, node):
        self._conditional(node)

    def visit_AsyncWith(self, node):
        self._conditional(node)

    def visit_Match(self, node):
        self._conditional(node)

    def visit_ExceptHandler(self, node):
        if node.name is not None:
            self._binding(node.name, node)
        self.generic_visit(node)

    def visit_MatchAs(self, node):
        if node.name is not None:
            self._binding(node.name, node)
        self.generic_visit(node)

    def visit_MatchStar(self, node):
        if node.name is not None:
            self._binding(node.name, node)

    def visit_MatchMapping(self, node):
        if node.rest is not None:
            self._binding(node.rest, node)
        self.generic_visit(node)

    def visit_Global(self, node):
        previous = self.scope
        self.scope = "module"
        for name in node.names:
            self._binding(name, node, conditional=True, flow=[])
        self.scope = previous

    def visit_Nonlocal(self, node):
        previous = self.scope
        scope = self.by_scope[self.scope]["parent"]
        while scope:
            self.scope = scope
            for name in node.names:
                self._binding(name, node, conditional=True, flow=[])
            scope = self.by_scope[scope]["parent"]
        self.scope = previous

    def _comprehension(self, node, outputs):
        self.visit(node.generators[0].iter)
        previous = self._enter_scope(node, "comprehension")
        for index, generator in enumerate(node.generators):
            if index:
                self.visit(generator.iter)
            self.visit(generator.target)
            for condition in generator.ifs:
                self.visit(condition)
        for output in outputs:
            self.visit(output)
        self.scope, self.conditional = previous

    def visit_ListComp(self, node):
        self._comprehension(node, [node.elt])

    def visit_SetComp(self, node):
        self._comprehension(node, [node.elt])

    def visit_GeneratorExp(self, node):
        self._comprehension(node, [node.elt])

    def visit_DictComp(self, node):
        self._comprehension(node, [node.key, node.value])


def type_facts(tree):
    visitor = _TypeVisitor()
    visitor.visit(tree)
    return {
        "scopes": visitor.scopes,
        "bindings": visitor.bindings,
        "reads": visitor.reads,
        "calls": visitor.calls,
    }
