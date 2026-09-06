from loader_bindings import LoaderBindings
from module_evidence import ModuleEvidence, module_name
from type_resolver import _Resolver

LOADERS = {"importlib.import_module", "builtins.__import__"}


def callable_names(module):
    names = {}
    for binding in module["facts"]["bindings"]:
        if binding["kind"] in {"function", "class"}:
            site = binding["site"]
            scope = (
                f"{binding['scope']}/{binding['kind']}:{site['line']}:{site['column']}"
            )
            names[scope] = binding["name"]
    result = {"module": "<module>"}
    for scope in module["scopes"]:
        if scope == "module":
            continue
        parent = result[scope.rpartition("/")[0]]
        name = names.get(
            scope, "<lambda>" if module["scopes"][scope]["kind"] == "function" else ""
        )
        result[scope] = (
            (parent + "." if parent != "<module>" and name else "") + name
            if name
            else parent
        )
    return result


def module_imports(modules, facts=None):
    facts = facts or _Resolver(modules)
    bindings = LoaderBindings(facts, ("__import__", "next"))
    result = []
    for path, module in sorted(facts.files.items()):
        evidence = ModuleEvidence(module, bindings)
        names = callable_names(module)
        calls = sorted(
            module["facts"]["calls"],
            key=lambda call: (call["site"]["line"], call["site"]["column"]),
        )
        for call in calls:
            if call["callee"]["kind"] != "name":
                continue
            candidates = bindings.candidates(
                module,
                call["scope"],
                call["callee"]["value"],
                call["site"],
                call["flow"],
            )
            loaders = candidates & LOADERS
            if not loaders:
                continue
            result.append(
                import_fact(
                    path,
                    call,
                    names[call["scope"]],
                    min(loaders),
                    candidates,
                    evidence,
                )
            )
    return result


def import_fact(path, call, callable_name, callee, candidates, resolver):
    arguments = call["arguments"]
    argument = arguments[0]["text"] if arguments else ""
    if not call["shape"] or len(argument.encode("utf-8")) > 4096:
        raise ValueError("Python module loader exceeds its bounded call evidence")
    error = ""
    evidence = None
    if candidates != {callee}:
        error = "module loader binding is ambiguous, conditional, or wildcard"
    elif len(arguments) != 1 or call["keywords"]:
        error = "module loader requires one positional argument"
    else:
        evidence = resolver.evidence(arguments[0])
        if evidence is None:
            error = "argument is outside the supported bounded computed-import shapes"
    targets, configuration, group = evidence or (set(), set(), "")
    type_only = path.endswith(".pyi") or any(
        resolver.bindings.reference(
            resolver.module, guard["scope"], guard["name"], guard["site"]
        )
        == "typing.TYPE_CHECKING"
        for guard in call["typeGuards"]
    )
    literal = (
        not error
        and arguments[0]["kind"] == "string"
        and module_name(arguments[0]["value"])
    )
    return {
        "path": path,
        "kind": "type-only" if type_only else "proven-dynamic",
        "literal": bool(literal),
        "callee": callee,
        "callable": callable_name,
        "line": call["site"]["line"],
        "column": call["site"]["column"],
        "endLine": call["endSite"]["line"],
        "endColumn": call["endSite"]["column"],
        "argument": argument,
        "shape": call["shape"],
        "targets": sorted(targets),
        "configuration": [
            {"path": path, "jsonPointer": pointer}
            for path, pointer in sorted(configuration)
        ],
        "entryPointGroup": group,
        "evidenceError": error,
    }
