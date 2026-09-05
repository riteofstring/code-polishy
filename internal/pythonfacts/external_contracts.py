from type_resolver import _exact, _name, _site

KINDS = {"base", "protocol", "decorator", "entry-point"}


def external_consumer(value):
    _exact(
        value,
        (
            "kind",
            "importer",
            "module",
            "site",
            "sourceSha256",
            "distribution",
            "qualified",
            "implementation",
            "member",
        ),
        "external reachability consumer",
    )
    if (
        value["kind"] not in KINDS
        or any(
            not _name(value[key])
            for key in ("module", "qualified", "implementation", "member")
        )
        or "." in value["member"]
    ):
        raise ValueError("external reachability consumer names are invalid")
    _site(value["site"])
    return value


def class_binding(resolver, module, name):
    scope = "module"
    binding = None
    for part in name.split("."):
        binding = resolver.exact_binding(module, scope, part)
        if binding["kind"] != "class":
            raise ValueError("external implementation is not one exact local class")
        scope = f"{scope}/class:{binding['site']['line']}:{binding['site']['column']}"
    return binding, scope


def qualified_matches(resolver, module, scope, expressions, qualified):
    return sum(
        expression["kind"] == "name"
        and resolver.facts.reference(module, scope, expression["name"]) == qualified
        for expression in expressions
    )


def registration(resolver, module, owner, expected):
    matches = []
    for call in module["facts"]["calls"]:
        if call["scope"] != "module" or call["site"] != expected["site"]:
            continue
        if resolver.callee(module, call) != expected["qualified"]:
            continue
        if len(call["arguments"]) != 1 or call["keywords"] or call["flow"]:
            continue
        argument = call["arguments"][0]
        if argument["kind"] != "name":
            continue
        actual = resolver.facts.reference(module, "module", argument["value"])
        if (
            actual == (module["path"], owner)
            and call["site"]["line"] > owner["endSite"]["line"]
        ):
            matches.append(call)
    if len(matches) != 1:
        raise ValueError("external entry-point registration is stale or disconnected")


def resolve_external_contract(resolver, request):
    declaration = request["declaration"]
    expected = external_consumer(declaration["consumer"])
    dependency = request["dependency"]
    _exact(dependency, ("distribution", "identity"), "admitted external dependency")
    if dependency["distribution"] != expected["distribution"] or (
        not isinstance(dependency["identity"], str)
        or len(dependency["identity"]) != 64
        or any(c not in "0123456789abcdef" for c in dependency["identity"])
    ):
        raise ValueError("external contract has no exact admitted dependency identity")
    module = resolver.runtime_module(expected["module"])
    if (
        module["path"] != expected["importer"]
        or module["sourceSha256"] != expected["sourceSha256"]
    ):
        raise ValueError("external implementation source path or digest is stale")
    root = expected["qualified"].split(".")[0]
    if "." not in expected["qualified"] or any(
        name.split(".")[0] == root for name in resolver.facts.modules
    ):
        raise ValueError("external contract resolves inside the governed project")
    owner, scope = class_binding(resolver, module, expected["implementation"])
    member = resolver.exact_binding(module, scope, expected["member"])
    if member["kind"] != "function":
        raise ValueError("external implementation member is not one exact method")
    kind = expected["kind"]
    if kind == "entry-point":
        registration(resolver, module, owner, expected)
    else:
        binding = member if kind == "decorator" else owner
        expressions = binding["decorators"] if kind == "decorator" else binding["bases"]
        if (
            binding["site"] != expected["site"]
            or qualified_matches(
                resolver, module, binding["scope"], expressions, expected["qualified"]
            )
            != 1
        ):
            raise ValueError(
                "external implementation contract is stale or disconnected"
            )
    target = declaration["target"]
    actual = resolver.target(target["module"], target["symbol"])
    definition = actual["definitions"][-1]
    if (definition["path"], definition["line"], definition["name"]) != (
        module["path"],
        member["definitionLine"],
        member["name"],
    ):
        raise ValueError("external contract does not consume the declared target")
    return [(target["module"], target["symbol"])]
