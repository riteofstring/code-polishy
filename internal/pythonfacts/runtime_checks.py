import json
import sys
from collections import Counter

from reachability import ReachabilityResolver, _definition, _identity, _scope_for
from type_resolver import (
    MAX_FACT_BYTES,
    MAX_MODULES,
    MAX_OUTPUT_BYTES,
    MAX_RECORD_BYTES,
    _exact,
    _name,
    _site,
    _unique_object,
)

PROTOCOLS = {"typing.Protocol", "typing_extensions.Protocol"}
RUNTIME_DECORATORS = {"typing.runtime_checkable", "typing_extensions.runtime_checkable"}


class RuntimeCheckResolver(ReachabilityResolver):
    def reference(self, module, scope, name):
        result = self.facts.reference(module, scope, name)
        if result is not None or name not in {"isinstance", "issubclass", "object"}:
            return result
        while scope:
            if (scope, name) in module["bindings"] or (scope, "*") in module[
                "bindings"
            ]:
                return None
            scope = module["scopes"][scope]["parent"]
        return "builtins." + name

    def call_reference(self, module, call):
        expression = call["callee"]
        return (
            self.reference(module, call["scope"], expression["value"])
            if expression["kind"] == "name"
            else None
        )

    def qualified(self, reference):
        if not isinstance(reference, tuple):
            raise TypeError("runtime contract has no exact governed definition")
        path, binding = reference
        module = self.facts.files[path]
        scope = self.callable_name(module, binding["scope"])
        parts = [module["module"]]
        if scope != "<module>":
            parts.append(scope)
        return ".".join([*parts, binding["name"]])

    def runtime_type(self, reference, kind, seen=frozenset(), selected=True):
        qualified = self.qualified(reference)
        if qualified in seen or len(seen) >= 128:
            raise ValueError("runtime type inheritance exceeds its resolution boundary")
        seen = seen | {qualified}
        path, binding = reference
        if (
            not path.endswith(".py")
            or binding["kind"] != "class"
            or not binding["runtimeClass"]
        ):
            raise ValueError("protocol is not one supported runtime class")
        module = self.facts.files[path]
        runtime = self.class_decorators(module, binding)
        protocol, data = self.class_bases(module, binding, kind, seen)
        scope = _scope_for(binding)
        data = data or any(
            member["kind"] in {"alias", "annotated"} or member["decorators"]
            for members in module["bindings"].values()
            for member in members
            if member["scope"] == scope
        )
        if selected and protocol and (not runtime or (kind == "issubclass" and data)):
            raise ValueError("protocol is not runtime-checkable for the selected check")
        return data

    def class_decorators(self, module, binding):
        runtime = False
        for decorator in binding["decorators"]:
            if decorator["kind"] != "name":
                raise ValueError("runtime class has an unsupported decorator")
            target = self.reference(module, binding["scope"], decorator["name"])
            if not isinstance(target, str) or target not in RUNTIME_DECORATORS:
                raise ValueError(
                    "runtime class has an unresolved or unsupported decorator"
                )
            runtime = True
        return runtime

    def class_bases(self, module, binding, kind, seen):
        protocol, data = False, False
        for base in binding["bases"]:
            if base["kind"] != "name":
                raise ValueError("runtime class base expression is unsupported")
            target = self.reference(module, binding["scope"], base["name"])
            if isinstance(target, str) and target in PROTOCOLS:
                protocol = True
            elif target not in ("abc.ABC", "builtins.object"):
                data = self.runtime_type(target, kind, seen, selected=False) or data
        return protocol, data

    def type_check(self, module, call, expected=None):
        callee = self.call_reference(module, call)
        if (
            callee not in ("builtins.isinstance", "builtins.issubclass")
            or len(call["arguments"]) != 2
            or call["keywords"]
            or not call["guard"]
            or call["arguments"][1]["kind"] != "name"
        ):
            raise ValueError(
                "runtime check is not one rejecting isinstance or issubclass guard"
            )
        kind = callee.rpartition(".")[2]
        protocol = self.reference(module, call["scope"], call["arguments"][1]["value"])
        if (
            expected is not None
            and self.facts.import_target(expected, frozenset()) != protocol
        ):
            raise ValueError("runtime check names a different protocol")
        self.runtime_type(protocol, kind)
        return kind, self.qualified(protocol)

    def validator_check(self, module, call, expected):
        reference = self.call_reference(module, call)
        if (
            not isinstance(reference, tuple)
            or self.facts.import_target(expected, frozenset()) != reference
            or len(call["arguments"]) != 1
            or call["keywords"]
            or not call["direct"]
            or call["conditional"]
        ):
            raise ValueError("validator call has no exact unconditional local contract")
        path, binding = reference
        if binding["kind"] != "function" or binding["validator"] is None:
            raise ValueError(
                "validator body is not one supported rejecting runtime check"
            )
        validator = self.facts.files[path]
        scope = _scope_for(binding)
        calls = [
            value
            for value in validator["facts"]["calls"]
            if value["scope"] == scope and value["site"] == binding["validator"]
        ]
        if len(calls) != 1:
            raise ValueError("validator has no unique rejecting check")
        _, protocol = self.type_check(validator, calls[0])
        value = calls[0]["arguments"][0]
        parameter = (
            self.facts.binding(validator, scope, value["value"])
            if value["kind"] == "name"
            else None
        )
        if (
            parameter is None
            or parameter["kind"] != "parameter"
            or parameter["scope"] != scope
        ):
            raise ValueError("validator checks a different value")
        return protocol

    def loaded_value(self, module, loader, check):
        argument = check["arguments"][0]
        if (
            argument["kind"] == "call"
            and argument["site"] == loader["site"]
            and argument["endSite"] == loader["endSite"]
        ):
            if check["statement"] != loader["statement"]:
                raise ValueError("inline loader is outside its runtime check")
            return []
        if argument["kind"] != "name" or loader["conditional"] or not loader["direct"]:
            raise ValueError("runtime check does not consume one direct loaded value")
        bindings = self.loaded_bindings(module, loader, argument["value"])
        positions = [binding["statement"] for binding in reversed(bindings)]
        if positions != list(range(loader["statement"], check["statement"])):
            raise ValueError("loaded value can escape or exit before its runtime check")
        return [_definition(module, binding) for binding in reversed(bindings)]

    def loaded_bindings(self, module, loader, name):
        seen, bindings = set(), []
        while name not in seen and len(seen) < 128:
            seen.add(name)
            binding = self.exact_binding(module, loader["scope"], name)
            bindings.append(binding)
            if (
                binding["valueSite"] == loader["site"]
                and binding["valueEndSite"] == loader["endSite"]
            ):
                return bindings
            if (
                binding["kind"] not in {"alias", "annotated"}
                or binding["value"]["kind"] != "name"
            ):
                break
            name = binding["value"]["name"]
        raise ValueError(
            "checked value is rebound, cyclic, or disconnected from the loader"
        )

    def resolve_check(self, request):
        _exact(request, ("id", "consumer", "check"), "runtime check request")
        expected = request["check"]
        _exact(expected, ("kind", "protocol", "site"), "runtime check contract")
        _site(expected["site"])
        if expected["kind"] not in {
            "isinstance",
            "issubclass",
            "validator-call",
        } or not _name(expected["protocol"]):
            raise ValueError("runtime check kind or protocol is unsupported")
        module, loader = self.consumer_binding(request["consumer"])
        matches = [
            call
            for call in module["facts"]["calls"]
            if call["scope"] == loader["scope"] and call["site"] == expected["site"]
        ]
        if len(matches) != 1 or matches[0]["statement"] < 0:
            raise ValueError("runtime check is missing, moved, or ambiguous")
        check = matches[0]
        if expected["kind"] == "validator-call":
            protocol = self.validator_check(module, check, expected["protocol"])
        else:
            kind, protocol = self.type_check(module, check, expected["protocol"])
            if kind != expected["kind"]:
                raise ValueError("runtime check shape changed")
        result = {
            "id": request["id"],
            "importer": module["path"],
            "loaderSite": loader["site"],
            "loaderEndSite": loader["endSite"],
            "checkSite": check["site"],
            "checkEndSite": check["endSite"],
            "kind": expected["kind"],
            "contract": expected["protocol"],
            "runtimeType": protocol,
            "bindings": self.loaded_value(module, loader, check),
        }
        result["identity"] = _identity(
            {"facts": self.identity, "request": request, "result": result}
        )
        return result


def resolve_stream(source, output):
    record = source.readline(MAX_RECORD_BYTES + 1)
    if len(record) > MAX_RECORD_BYTES or not record.endswith(b"\n"):
        raise ValueError("runtime check header exceeds its transport boundary")
    header = json.loads(record, object_pairs_hook=_unique_object)
    _exact(header, ("protocol", "count", "requests"), "runtime check header")
    if (
        header["protocol"] != "python-runtime-check-project/v1"
        or type(header["count"]) is not int
        or not 0 <= header["count"] <= MAX_MODULES
        or not isinstance(header["requests"], list)
    ):
        raise ValueError("runtime check header is invalid")
    modules, size = [], 0
    for _ in range(header["count"]):
        record = source.readline(MAX_RECORD_BYTES + 1)
        size += len(record)
        if (
            len(record) > MAX_RECORD_BYTES
            or size > MAX_FACT_BYTES
            or not record.endswith(b"\n")
        ):
            raise ValueError("runtime check project exceeds its transport boundary")
        modules.append(json.loads(record, object_pairs_hook=_unique_object))
    if source.read(1):
        raise ValueError("runtime check project has trailing input")
    evidence, problems = resolve_runtime_checks(modules, header["requests"])
    result = {
        "protocol": header["protocol"],
        "covered": [
            {"path": value["path"], "sourceSha256": value["sourceSha256"]}
            for value in modules
        ],
        "evidence": evidence,
        "problems": problems,
    }
    encoded = json.dumps(
        result, ensure_ascii=False, sort_keys=True, separators=(",", ":")
    ).encode("utf-8")
    if len(encoded) > MAX_OUTPUT_BYTES:
        raise ValueError("runtime check output exceeds its response boundary")
    output.write(encoded)


def resolve_runtime_checks(modules, requests, resolver=None):
    resolver = resolver or RuntimeCheckResolver(modules)
    evidence, problems = [], []
    consumers = Counter(_consumer_identity(request) for request in requests)
    for request in requests:
        try:
            if consumers[_consumer_identity(request)] != 1:
                raise ValueError("loader has duplicate runtime check declarations")
            evidence.append(resolver.resolve_check(request))
        except (ValueError, TypeError, KeyError, IndexError, RecursionError) as error:
            problems.append({"id": request["id"], "message": str(error)[:4096]})
    return evidence, problems


def _consumer_identity(request):
    consumer = request["consumer"]
    return _identity(
        {"importer": consumer.get("importer"), "site": consumer.get("site")}
    )


if __name__ == "__main__":
    resolve_stream(sys.stdin.buffer, sys.stdout.buffer)
