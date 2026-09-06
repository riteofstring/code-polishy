import ast

from source_parser import parse_source

GRAMMAR = r"[A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)*"
PATTERN = GRAMMAR + ":" + GRAMMAR


def require(condition, message):
    if not condition:
        raise ValueError(message)


def site(node):
    return {"line": node.lineno, "column": node.col_offset + 1}


def equal(node, text):
    return ast.dump(node) == ast.dump(ast.parse(text, mode="eval").body)


def assigned(statement):
    if isinstance(statement, ast.AnnAssign):
        return statement.target, statement.value
    require(
        isinstance(statement, ast.Assign) and len(statement.targets) == 1,
        "loader requires one direct assignment",
    )
    return statement.targets[0], statement.value


def rejection(node: ast.stmt) -> ast.If:
    if not isinstance(node, ast.If):
        raise TypeError("guard must reject with raise")
    require(
        not node.orelse and len(node.body) == 1 and isinstance(node.body[0], ast.Raise),
        "guard must reject with raise",
    )
    return node


def loader_syntax(source, consumer, check):
    tree = parse_source(consumer["importer"], source)
    functions = [
        node
        for node in ast.walk(tree)
        if isinstance(node, ast.FunctionDef)
        and any(
            isinstance(call, ast.Call) and site(call) == consumer["site"]
            for call in ast.walk(node)
        )
    ]
    require(len(functions) == 1, "loader must be one non-nested synchronous function")
    function = functions[0]
    require(
        not function.decorator_list and len(function.body) == 6,
        "loader requires guard, split, import, attribute walk, check, return",
    )
    parameters = function.args.posonlyargs + function.args.args
    require(
        len(parameters) == 1
        and not function.args.vararg
        and not function.args.kwarg
        and not function.args.kwonlyargs,
        "loader must take one target parameter",
    )
    parameter = parameters[0].arg
    guard, split, imported, walk, checked, returned = function.body
    regex = grammar_guard(tree, guard, parameter)
    split_target, split_value = assigned(split)
    require(
        isinstance(split_target, ast.Tuple)
        and len(split_target.elts) == 2
        and all(isinstance(value, ast.Name) for value in split_target.elts),
        "loader must split module and attribute names",
    )
    module_name, object_name = [value.id for value in split_target.elts]
    require(
        equal(split_value, f'{parameter}.split(":", maxsplit=1)')
        or equal(split_value, f'{parameter}.split(":", 1)'),
        "target split changed",
    )
    loaded, call = assigned(imported)
    require(
        isinstance(loaded, ast.Name)
        and isinstance(call, ast.Call)
        and site(call) == consumer["site"]
        and len(call.args) == 1
        and equal(call.args[0], module_name),
        "import is disconnected from target",
    )
    require(
        len({parameter, module_name, object_name, loaded.id}) == 4,
        "loader aliases overwrite target bindings",
    )
    walked = attribute_walk(walk, object_name, loaded.id)
    checked = rejection(checked)
    require(
        isinstance(checked.test, ast.UnaryOp)
        and isinstance(checked.test.op, ast.Not)
        and isinstance(checked.test.operand, ast.Call)
        and site(checked.test.operand) == check["site"]
        and equal(checked.test.operand.args[0], loaded.id),
        "check consumes a different object",
    )
    require(
        isinstance(returned, ast.Return) and equal(returned.value, loaded.id),
        "loader must return its checked object",
    )
    return regex, walked


def grammar_guard(tree, guard, parameter):
    rejection(guard)
    test = guard.test
    require(
        isinstance(test, ast.Compare)
        and len(test.ops) == 1
        and isinstance(test.ops[0], ast.Is)
        and equal(test.comparators[0], "None"),
        "grammar guard must reject a failed fullmatch",
    )
    call = test.left
    require(
        isinstance(call, ast.Call)
        and isinstance(call.func, ast.Attribute)
        and call.func.attr == "fullmatch"
        and isinstance(call.func.value, ast.Name)
        and len(call.args) == 1
        and equal(call.args[0], parameter)
        and not call.keywords,
        "grammar guard must fullmatch the target parameter",
    )
    name = call.func.value.id
    definitions = []
    for statement in tree.body:
        targets = (
            statement.targets
            if isinstance(statement, ast.Assign)
            else [statement.target]
            if isinstance(statement, ast.AnnAssign)
            else []
        )
        if any(
            isinstance(target, ast.Name) and target.id == name for target in targets
        ):
            _, value = assigned(statement)
            definitions.append(value)
    require(len(definitions) == 1, "grammar expression has no unique module definition")
    regex = definitions[0]
    require(
        isinstance(regex, ast.Call)
        and len(regex.args) == 1
        and not regex.keywords
        and isinstance(regex.args[0], ast.Constant)
        and regex.args[0].value == PATTERN,
        "grammar must use ascii-module-object/v1 without regex flags",
    )
    return name, regex


def attribute_walk(walk, object_name, loaded):
    require(
        isinstance(walk, ast.For)
        and isinstance(walk.target, ast.Name)
        and not walk.orelse
        and len(walk.body) == 1
        and equal(walk.iter, f'{object_name}.split(".")'),
        "loader requires a direct nested attribute walk",
    )
    target, value = assigned(walk.body[0])
    require(
        isinstance(target, ast.Name)
        and target.id == loaded
        and equal(value, f"getattr({loaded}, {walk.target.id})")
        and walk.target.id not in {object_name, loaded},
        "attribute walk is disconnected",
    )
    return value
