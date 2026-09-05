import sys

from reachability import _definition, _scope_for


def object_guard(resolver, module, loader):
    argument = loader["arguments"][0]
    if argument["kind"] != "name":
        raise ValueError("object input has no supported grammar guard")
    binding = resolver.exact_binding(module, loader["scope"], argument["value"])
    if binding["kind"] != "parameter":
        raise ValueError("guarded object input is not one current callable parameter")
    candidates = _guard_calls(resolver, module).get(
        (loader["scope"], argument["value"]), []
    )
    resolver.loader_bindings.remaining -= len(candidates)
    if resolver.loader_bindings.remaining < 0:
        raise ValueError("object input guard exceeds its resolution work boundary")
    matches = []
    for guard in candidates:
        if not 0 <= guard["statement"] < loader["statement"]:
            continue
        target = resolver.callee(module, guard)
        if not isinstance(target, tuple):
            continue
        path, predicate = target
        if not path.endswith(".py") or predicate["objectPredicate"] is None:
            continue
        proof = predicate["objectPredicate"]
        _predicate_references(resolver, resolver.facts.files[path], predicate)
        matches.append(
            {
                "namespace": proof["namespace"],
                "standardLibrary": proof["namespace"].partition(".")[0]
                in sys.stdlib_module_names,
                "site": guard["site"],
                "endSite": guard["endSite"],
                "predicate": _definition(resolver.facts.files[path], predicate),
                "predicateSha256": resolver.facts.files[path]["sourceSha256"],
            }
        )
    if len(matches) != 1:
        raise ValueError(
            "object input has no unique rejecting grammar and namespace guard"
        )
    return matches[0]


def _guard_calls(resolver, module):
    if module["path"] not in resolver.guard_calls:
        indexed = {}
        for call in module["facts"]["calls"]:
            if (
                call["guard"]
                and len(call["arguments"]) == 1
                and not call["keywords"]
                and call["arguments"][0]["kind"] == "name"
            ):
                key = call["scope"], call["arguments"][0]["value"]
                indexed.setdefault(key, []).append(call)
        resolver.guard_calls[module["path"]] = indexed
    return resolver.guard_calls[module["path"]]


def _predicate_references(resolver, module, predicate):
    expected = {
        "type": "builtins.type",
        "str": "builtins.str",
        "normalize": "unicodedata.normalize",
        "all": "builtins.all",
        "keyword": "keyword.iskeyword",
    }
    scope = _scope_for(predicate)
    for key, name in predicate["objectPredicate"]["references"].items():
        reference = _reference(resolver, module, scope, name)
        if reference != expected[key]:
            raise ValueError("object predicate uses a shadowed or unresolved operation")


def _reference(resolver, module, scope, name):
    reference = resolver.facts.reference(module, scope, name)
    if reference is not None or name not in {"type", "str", "all"}:
        return reference
    while scope:
        if (scope, name) in module["bindings"] or (scope, "*") in module["bindings"]:
            return None
        scope = module["scopes"][scope]["parent"]
    return "builtins." + name
