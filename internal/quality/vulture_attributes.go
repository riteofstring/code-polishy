package quality

const pythonVultureExternalAttributes = `
def _external_location(value):
    if not isinstance(value, dict) or set(value) != {"line", "column"}:
        raise ValueError("invalid external-write source location")
    if any(type(value[key]) is not int or value[key] < 1 for key in value):
        raise ValueError("invalid external-write source location")

def _validate_external_receiver(receiver):
    if not isinstance(receiver, dict):
        raise ValueError("invalid external-write receiver")
    kind = receiver.get("kind")
    fields = {"kind", "name", "binding"}
    if kind in ("parameter", "local"):
        fields.add("type")
        if not n(receiver.get("type")) or "." not in receiver["type"]:
            raise ValueError("invalid external-write type")
    elif kind == "self":
        fields.add("consumer")
        consumer = receiver.get("consumer")
        if not isinstance(consumer, dict) or set(consumer) != {"kind", "qualified", "site"}:
            raise ValueError("invalid external-write consumer")
        if consumer["kind"] not in ("base", "protocol", "decorator", "registration"):
            raise ValueError("invalid external-write consumer kind")
        if not n(consumer["qualified"]) or "." not in consumer["qualified"]:
            raise ValueError("invalid external-write consumer name")
        _external_location(consumer["site"])
    else:
        raise ValueError("invalid external-write receiver kind")
    if set(receiver) != fields or not n(receiver["name"]) or "." in receiver["name"]:
        raise ValueError("invalid external-write receiver fields")
    _external_location(receiver["binding"])

def _external_at(node, site):
    return node.lineno == site["line"] and node.col_offset + 1 == site["column"]

def _external_scope_nodes(node, parent=None):
    yield node, parent
    if isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef, ast.Lambda)):
        expressions = list(node.args.defaults) + [value for value in node.args.kw_defaults if value is not None]
        if not isinstance(node, ast.Lambda):
            expressions += node.decorator_list
            expressions += [argument.annotation for argument in _external_parameters(node) if argument.annotation is not None]
            if node.returns is not None:
                expressions.append(node.returns)
        for expression in expressions:
            yield from _external_scope_nodes(expression, node)
        return
    if isinstance(node, ast.ClassDef):
        for expression in node.bases + node.decorator_list + [keyword.value for keyword in node.keywords]:
            yield from _external_scope_nodes(expression, node)
        return
    for child in ast.iter_child_nodes(node):
        yield from _external_scope_nodes(child, node)

def _external_definitions(body, name):
    result = []
    for statement in body:
        for node, parent in _external_scope_nodes(statement):
            binding = None
            if isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef, ast.ClassDef)) and node.name == name:
                binding = node
            elif isinstance(node, (ast.Import, ast.ImportFrom)):
                for alias in node.names:
                    if alias.name == "*":
                        raise ValueError("wildcard import cannot prove an external receiver")
                    if (alias.asname or alias.name.split(".")[0]) == name:
                        result.append((node, alias, node is statement))
            elif isinstance(node, ast.Name) and isinstance(node.ctx, (ast.Store, ast.Del)) and node.id == name:
                binding = parent
            elif isinstance(node, ast.ExceptHandler) and node.name == name:
                binding = node
            elif isinstance(node, (ast.MatchAs, ast.MatchStar)) and node.name == name:
                binding = node
            elif isinstance(node, ast.MatchMapping) and node.rest == name:
                binding = node
            elif isinstance(node, (ast.Global, ast.Nonlocal)) and name in node.names:
                binding = node
            if binding is not None:
                result.append((binding, None, binding is statement))
    return result

def _external_parts(node):
    if isinstance(node, ast.Name):
        return [node.id]
    if isinstance(node, ast.Attribute):
        return _external_parts(node.value) + [node.attr]
    raise ValueError("external receiver requires an exact type or consumer expression")

def _external_qualified(f, node, scopes=(), seen=frozenset()):
    parts = _external_parts(node)
    identity = (f["path"], tuple(parts), tuple(id(scope) for scope in scopes))
    if identity in seen or len(seen) >= 64:
        raise ValueError("external receiver alias is cyclic or exceeds resolution depth")
    seen = seen | {identity}
    for scope in (*scopes, f["tree"].body):
        bindings = _external_definitions(scope, parts[0])
        if not bindings:
            continue
        if len(bindings) != 1 or not bindings[0][2]:
            raise ValueError("external receiver type or consumer is shadowed or ambiguous")
        binding, alias, _ = bindings[0]
        if isinstance(binding, ast.Import):
            qualified = alias.name if alias.asname else alias.name.split(".")[0]
        elif isinstance(binding, ast.ImportFrom):
            if binding.level and (not f["package"] or binding.level > len(f["package"].split("."))):
                raise ValueError("external receiver re-export escapes its project")
            module = fm(f, binding)
            if not module:
                raise ValueError("external receiver re-export escapes its project")
            qualified = module + "." + alias.name
        elif isinstance(binding, ast.Assign) and len(binding.targets) == 1 and isinstance(binding.targets[0], ast.Name):
            qualified = _external_qualified(f, binding.value, scopes, seen)
        else:
            raise ValueError("external receiver type or consumer is not an exact external import")
        qualified += "".join("." + part for part in parts[1:])
        return _external_import_target(qualified, seen)
    raise ValueError("external receiver type or consumer has no exact import")

def _external_import_target(qualified, seen):
    parts = qualified.split(".")
    local = {f["module"] for f in fs if f["module"]}
    for index in range(len(parts), 0, -1):
        module = ".".join(parts[:index])
        if module not in local:
            continue
        targets = mods.get(module, [])
        if len(targets) != 1 or index == len(parts):
            raise ValueError("external receiver re-export is local or ambiguous")
        expression = ast.Name(id=parts[index])
        for part in parts[index + 1:]:
            expression = ast.Attribute(value=expression, attr=part)
        return _external_qualified(targets[0], expression, (), seen)
    if any(module.split(".")[0] == parts[0] for module in local):
        raise ValueError("external receiver import is an unresolved local namespace")
    if qualified in ("typing.Any", "typing_extensions.Any"):
        raise ValueError("Any cannot prove an external receiver")
    return qualified

def _external_parameters(node):
    args = node.args
    return args.posonlyargs + args.args + args.kwonlyargs + ([args.vararg] if args.vararg else []) + ([args.kwarg] if args.kwarg else [])

def _external_callable(attribute):
    f, node = fc(attribute["module"], attribute["callable"].split("."))
    body = f["tree"].body
    owner = None
    for name in attribute["callable"].split("."):
        bindings = _external_definitions(body, name)
        if len(bindings) != 1 or not bindings[0][2]:
            raise ValueError("external write callable is shadowed or ambiguous")
        current = bindings[0][0]
        if current is node:
            return f, node, owner
        if not isinstance(current, ast.ClassDef):
            raise ValueError("external write callable has no exact class owner")
        owner = current
        body = current.body
    raise ValueError("external write callable is stale or ambiguous")

def _external_assignment(f, node, attribute):
    receiver = attribute["receiver"]
    writes = [item for item in wn(node) if isinstance(item, ast.Attribute) and isinstance(item.ctx, ast.Store)
              and item.attr == attribute["attribute"] and _external_at(item, attribute["write"])
              and isinstance(item.value, ast.Name) and item.value.id == receiver["name"]]
    if len(writes) != 1:
        raise ValueError("external attribute write is stale or ambiguous")
    write = writes[0]
    parents = {id(child): parent for parent in ast.walk(f["tree"]) for child in ast.iter_child_nodes(parent)}
    statement = parents[id(write)]
    if not isinstance(statement, (ast.Assign, ast.AnnAssign)):
        raise ValueError("external write must be one attribute assignment")
    identity = c(f, write, attribute["attribute"])
    collisions = [item for item in ast.walk(f["tree"]) if isinstance(item, ast.Attribute)
                  and isinstance(item.ctx, ast.Store) and c(f, item, item.attr) == identity]
    if len(collisions) != 1:
        raise ValueError("external write cannot distinguish same-line Vulture occurrences")
    return write, statement

def _external_receiver_binding(node, write, statement, receiver):
    name = receiver["name"]
    if any(isinstance(item, ast.Nonlocal) and name in item.names for item in ast.walk(node)):
        raise ValueError("external write receiver has an ambiguous nonlocal binding")
    parameters = [argument for argument in _external_parameters(node) if argument.arg == name]
    bindings = _external_definitions(node.body, name)
    kind = receiver["kind"]
    if kind == "local":
        selected = [binding for binding, _, direct in bindings if direct and isinstance(binding, ast.AnnAssign)
                    and isinstance(binding.target, ast.Name) and _external_at(binding.target, receiver["binding"])]
        if parameters or len(selected) != 1 or selected[0].value is None:
            raise ValueError("external write has no exact annotated local binding")
        binding = selected[0]
        if (binding.lineno, binding.col_offset) >= (write.lineno, write.col_offset):
            raise ValueError("external write precedes its annotated local binding")
    else:
        if len(parameters) != 1 or not _external_at(parameters[0], receiver["binding"]):
            raise ValueError("external write has no exact receiver parameter binding")
        binding = parameters[0]
    for other, _, direct in bindings:
        if other is binding:
            continue
        if not isinstance(other, (ast.Global, ast.Nonlocal)) and direct and statement in node.body and (other.lineno, other.col_offset) > (write.lineno, write.col_offset):
            continue
        raise ValueError("external write receiver is shadowed or rebound before the assignment")
    return binding

def _external_receiver_annotation(f, node, owner, binding, receiver):
    if binding.annotation is None:
        raise ValueError("external write receiver has no exact type annotation")
    scopes = (owner.body,) if owner is not None else ()
    if receiver["kind"] == "local":
        root = _external_parts(binding.annotation)[0]
        if any(argument.arg == root for argument in _external_parameters(node)):
            raise ValueError("external receiver annotation is shadowed by a parameter")
        scopes = (node.body, *scopes)
    if _external_qualified(f, binding.annotation, scopes) != receiver["type"]:
        raise ValueError("external write receiver annotation does not match its external type")

def _external_self_consumer(f, node, owner, binding, receiver):
    positional = node.args.posonlyargs + node.args.args
    if owner is None or not positional or positional[0] is not binding or node.decorator_list:
        raise ValueError("self write requires the exact receiver of an undecorated instance method")
    consumer = receiver["consumer"]
    kind = consumer["kind"]
    if kind in ("base", "protocol"):
        candidates = owner.bases
    elif kind == "decorator":
        candidates = owner.decorator_list
    else:
        candidates = _external_registration_calls(f, owner)
    matches = []
    for candidate in candidates:
        expression = candidate.func if kind == "registration" else candidate
        try:
            qualified = _external_qualified(f, expression)
        except ValueError:
            continue
        if qualified == consumer["qualified"]:
            matches.append(candidate)
    if len(matches) != 1 or not _external_at(matches[0], consumer["site"]):
        raise ValueError("self write external consumer is stale or ambiguous")

def _external_registration_calls(f, owner):
    bindings = _external_definitions(f["tree"].body, owner.name)
    if len(bindings) != 1 or bindings[0][0] is not owner or not bindings[0][2]:
        raise ValueError("registered class has no exact module binding")
    calls = []
    for statement in f["tree"].body:
        if not isinstance(statement, ast.Expr) or not isinstance(statement.value, ast.Call):
            continue
        call = statement.value
        if call.lineno > owner.end_lineno and len(call.args) == 1 and not call.keywords and isinstance(call.args[0], ast.Name) and call.args[0].id == owner.name:
            calls.append(call)
    return calls

def _external_write(attribute):
    f, node, owner = _external_callable(attribute)
    receiver = attribute["receiver"]
    write, statement = _external_assignment(f, node, attribute)
    binding = _external_receiver_binding(node, write, statement, receiver)
    if receiver["kind"] == "self":
        _external_self_consumer(f, node, owner, binding, receiver)
    else:
        _external_receiver_annotation(f, node, owner, binding, receiver)
    return c(f, write, attribute["attribute"])
`
