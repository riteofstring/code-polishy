import json
import sys

from reachability import _definition, _definition_key, _identity, _scope_for
from type_resolver import (
    MAX_FACT_BYTES,
    MAX_MODULES,
    MAX_OUTPUT_BYTES,
    MAX_RECORD_BYTES,
    _exact,
    _name,
    _Resolver,
    _unique_object,
)


class ContractDefinitions:
    def __init__(self, modules):
        self.facts = _Resolver(modules)
        for name, candidates in self.facts.modules.items():
            runtime = [value for value in candidates if value["path"].endswith(".py")]
            chosen = runtime or [
                value for value in candidates if value["path"].endswith(".pyi")
            ]
            if len(chosen) != 1:
                raise ValueError("dependency contract module is ambiguous")
            self.facts.modules[name] = chosen
        self.identity = _identity(sorted(modules, key=lambda value: value["path"]))

    def definition(self, reference, kind):
        if reference is None or isinstance(reference, str):
            raise ValueError("contract definition is outside its owning distribution")
        module, binding = self.facts.files[reference[0]], reference[1]
        self.facts.require_unique_definition(module, binding)
        if binding["kind"] != kind:
            raise ValueError("dependency contract has the wrong definition kind")
        return module, binding

    def member(self, reference, name, seen=frozenset()):
        module, binding = self.definition(reference, "class")
        definition = _definition(module, binding)
        key = _definition_key(definition)
        if key in seen or len(seen) >= 128:
            raise ValueError("dependency contract inheritance is cyclic or too deep")
        scope = _scope_for(binding)
        own = self.facts.reference(module, scope, name)
        if module["bindings"].get((scope, name)):
            owner, method = self.definition(own, "function")
            if method["scope"] == scope and owner["path"] == module["path"]:
                return [definition, _definition(owner, method)]
        inherited = []
        for base in binding["bases"]:
            if base["kind"] != "name":
                raise ValueError("dependency contract base expression is unsupported")
            resolved = self.facts.reference(module, binding["scope"], base["name"])
            if resolved in (None, "typing.Protocol", "typing.Generic"):
                continue
            inherited.append(self.member(resolved, name, seen | {key}))
        if len(inherited) != 1:
            raise ValueError("dependency contract member is absent or ambiguous")
        return [definition, *inherited[0]]

    def protocol(self, reference, seen=frozenset()):
        module, binding = self.definition(reference, "class")
        key = _definition_key(_definition(module, binding))
        if key in seen or len(seen) >= 128:
            raise ValueError("dependency protocol inheritance is cyclic or too deep")
        for base in binding["bases"]:
            if base["kind"] != "name":
                continue
            resolved = self.facts.reference(module, binding["scope"], base["name"])
            if resolved == "typing.Protocol":
                return True
            if isinstance(resolved, tuple) and self.protocol(resolved, seen | {key}):
                return True
        return False

    def registration_contract(self, reference):
        module, function = self.definition(reference, "function")
        scope = _scope_for(function)
        parameters = [
            binding
            for values in module["bindings"].values()
            for binding in values
            if binding["scope"] == scope and binding["kind"] == "parameter"
        ]
        if len(parameters) != 1:
            raise ValueError("registration requires one exact typed class parameter")
        parameter = parameters[0]
        annotation = parameter["annotation"]
        annotation_scope = parameter["annotationScope"]
        constructor = self.facts.reference(module, annotation_scope, annotation["name"])
        builtin = (
            annotation["name"] == "type"
            and self.facts.binding(module, annotation_scope, "type") is None
        )
        if (
            annotation["kind"] != "subscript"
            or len(annotation["args"]) != 1
            or not (builtin or constructor == "typing.Type")
        ):
            raise ValueError("registration parameter requires type[Contract]")
        argument = annotation["args"][0]
        if argument["kind"] != "name":
            raise ValueError("registration contract type is unsupported")
        return _definition(module, function), self.facts.reference(
            module, annotation_scope, argument["name"]
        )

    def resolve(self, request):
        _exact(
            request,
            ("id", "kind", "qualified", "member"),
            "dependency contract request",
        )
        if (
            not _name(request["qualified"])
            or not _name(request["member"])
            or "." in request["member"]
        ):
            raise ValueError("dependency contract names are invalid")
        reference = self.facts.import_target(request["qualified"], frozenset())
        kind = request["kind"]
        if kind == "protocol" and not self.protocol(reference):
            raise ValueError("dependency contract is not a proven typing.Protocol")
        if kind in {"base", "protocol"}:
            definitions = self.member(reference, request["member"])
        elif kind == "decorator":
            module, function = self.definition(reference, "function")
            definitions = [_definition(module, function)]
        elif kind == "entry-point":
            registration, contract = self.registration_contract(reference)
            definitions = [registration, *self.member(contract, request["member"])]
        else:
            raise ValueError("dependency contract kind is unsupported")
        return {
            **request,
            "definitions": definitions,
            "identity": _identity(
                {"facts": self.identity, "request": request, "definitions": definitions}
            ),
        }


def resolve_stream(source, output):
    header = json.loads(
        source.readline(MAX_RECORD_BYTES + 1), object_pairs_hook=_unique_object
    )
    _exact(header, ("protocol", "count", "requests"), "dependency contract header")
    if (
        header["protocol"] != "python-contract-definitions/v1"
        or type(header["count"]) is not int
        or not 0 <= header["count"] <= MAX_MODULES
        or not isinstance(header["requests"], list)
        or len(header["requests"]) > 4096
    ):
        raise ValueError("dependency contract header is invalid")
    modules, size = [], 0
    for _ in range(header["count"]):
        record = source.readline(MAX_RECORD_BYTES + 1)
        size += len(record)
        if (
            len(record) > MAX_RECORD_BYTES
            or size > MAX_FACT_BYTES
            or not record.endswith(b"\n")
        ):
            raise ValueError(
                "dependency contract facts exceed their transport boundary"
            )
        modules.append(json.loads(record, object_pairs_hook=_unique_object))
    if source.read(1):
        raise ValueError("dependency contract input has trailing data")
    resolver = ContractDefinitions(modules)
    evidence, problems, seen = [], [], set()
    for request in header["requests"]:
        if request["id"] in seen:
            raise ValueError("dependency contract request is duplicated")
        seen.add(request["id"])
        try:
            evidence.append(resolver.resolve(request))
        except (ValueError, TypeError, KeyError, IndexError, RecursionError) as error:
            problems.append({"id": request["id"], "message": str(error)[:4096]})
    result = {
        "protocol": header["protocol"],
        "evidence": evidence,
        "problems": problems,
        "covered": [
            {"path": value["path"], "sourceSha256": value["sourceSha256"]}
            for value in modules
        ],
    }
    encoded = json.dumps(
        result, ensure_ascii=False, sort_keys=True, separators=(",", ":")
    ).encode("utf-8")
    if len(encoded) > MAX_OUTPUT_BYTES:
        raise ValueError("dependency contract response exceeds its byte boundary")
    output.write(encoded)


if __name__ == "__main__":
    resolve_stream(sys.stdin.buffer, sys.stdout.buffer)
