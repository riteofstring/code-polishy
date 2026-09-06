import hashlib
import json
import re
import sys

from runtime_checks import RuntimeCheckResolver
from runtime_loader_syntax import PATTERN, loader_syntax, require, site
from type_resolver import MAX_FACT_BYTES, MAX_MODULES, MAX_RECORD_BYTES, _unique_object


def call_at(module, node):
    values = [call for call in module["facts"]["calls"] if call["site"] == site(node)]
    require(len(values) == 1, "loader operation has no unique source fact")
    return values[0]


def resolve_loader(resolver, request):
    consumer = request["consumer"]
    module, loader = resolver.callsite_binding(consumer)
    require(
        hashlib.sha256(request["source"].encode()).hexdigest()
        == module["sourceSha256"],
        "loader source snapshot is stale",
    )
    require(
        request["inputGrammar"] == "ascii-module-object/v1",
        "unsupported loader grammar",
    )
    (regex_name, regex), walked = loader_syntax(
        request["source"], consumer, request["check"]
    )
    regex_binding = resolver.exact_binding(module, "module", regex_name)
    require(
        regex_binding["valueSite"] == site(regex)
        and resolver.reference(module, loader["scope"], regex_name)
        == (module["path"], regex_binding),
        "grammar binding is shadowed or changed",
    )
    require(
        resolver.call_reference(module, call_at(module, regex)) == "re.compile",
        "grammar compiler is shadowed or unresolved",
    )
    walk = call_at(module, walked)
    require(
        resolver.reference(module, walk["scope"], "getattr") is None
        and not any(name == "getattr" or name == "*" for _, name in module["bindings"]),
        "attribute walker is shadowed or unresolved",
    )
    matches = [
        call
        for call in module["facts"]["calls"]
        if call["site"] == request["check"]["site"] and call["scope"] == loader["scope"]
    ]
    require(len(matches) == 1, "runtime check is missing or ambiguous")
    kind, protocol = resolver.type_check(
        module, matches[0], request["check"]["protocol"]
    )
    require(kind == request["check"]["kind"], "runtime check kind changed")
    return known_targets(resolver, module, consumer), protocol


def known_targets(resolver, module, consumer):
    reference = resolver.facts.import_target(
        module["module"] + "." + consumer["callable"], frozenset()
    )
    require(isinstance(reference, tuple), "loader callable is not uniquely resolved")
    targets = []
    for candidate in resolver.facts.files.values():
        for call in candidate["facts"]["calls"]:
            if resolver.call_reference(candidate, call) != reference:
                continue
            arguments = call["arguments"]
            if (
                len(arguments) != 1
                or call["keywords"]
                or arguments[0]["kind"] != "string"
            ):
                continue
            value = arguments[0]["value"]
            if re.fullmatch(PATTERN, value) is None:
                continue
            module_name, symbol = value.split(":")
            targets.append(
                {
                    "path": candidate["path"],
                    "module": module_name,
                    "symbol": symbol,
                    "site": call["site"],
                }
            )
    return targets


def resolve_stream():
    header = sys.stdin.buffer.readline(MAX_RECORD_BYTES + 1)
    require(
        len(header) <= MAX_RECORD_BYTES and header.endswith(b"\n"),
        "loader header exceeds boundary",
    )
    request = json.loads(header, object_pairs_hook=_unique_object)
    require(
        request["protocol"] == "python-runtime-loader-project/v1"
        and 0 <= request["count"] <= MAX_MODULES,
        "invalid loader protocol",
    )
    modules, size = [], 0
    for _ in range(request["count"]):
        record = sys.stdin.buffer.readline(MAX_RECORD_BYTES + 1)
        size += len(record)
        require(
            len(record) <= MAX_RECORD_BYTES
            and size <= MAX_FACT_BYTES
            and record.endswith(b"\n"),
            "loader facts exceed boundary",
        )
        modules.append(json.loads(record, object_pairs_hook=_unique_object))
    require(not sys.stdin.buffer.read(1), "loader facts contain trailing input")
    resolver = RuntimeCheckResolver(modules)
    result = {"error": "", "targets": [], "runtimeType": ""}
    try:
        result["targets"], result["runtimeType"] = resolve_loader(
            resolver, request["request"]
        )
    except (
        ValueError,
        TypeError,
        KeyError,
        IndexError,
        AttributeError,
        RecursionError,
    ) as error:
        result["error"] = str(error)[:4096]
    sys.stdout.write(json.dumps(result, sort_keys=True, separators=(",", ":")))


if __name__ == "__main__":
    resolve_stream()
