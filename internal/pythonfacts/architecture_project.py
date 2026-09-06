import json
import sys
import tokenize

from module_imports import module_imports
from object_imports import ObjectImportResolver
from runtime_checks import RuntimeCheckResolver, resolve_runtime_checks
from runtime_loaders import resolve_loader
from source_adapter import _source
from type_resolver import (
    MAX_FACT_BYTES,
    MAX_MODULES,
    MAX_RECORD_BYTES,
    _exact,
    _Resolver,
    _unique_object,
    typed_dict_reads,
)


def _sources(source, count):
    sources = []
    modules = []
    size = 0
    for _ in range(count):
        record = source.readline(MAX_RECORD_BYTES + 1)
        size += len(record)
        if (
            len(record) > MAX_RECORD_BYTES
            or size > 512 * 1024 * 1024
            or not record.endswith(b"\n")
        ):
            raise ValueError("architecture project exceeds its transport boundary")
        value = json.loads(record, object_pairs_hook=_unique_object)
        _exact(value, ("path", "module", "package", "source"), "architecture source")
        try:
            parsed = _source(value["path"], value["source"])
        except (
            ValueError,
            TypeError,
            SyntaxError,
            tokenize.TokenError,
            RecursionError,
        ) as error:
            raise ValueError(f"parse {value['path']}: {error}") from None
        sources.append(parsed)
        modules.append(
            {
                "path": parsed["path"],
                "module": value["module"],
                "package": value["package"],
                "sourceSha256": parsed["sha256"],
                "facts": parsed["typeFacts"],
            }
        )
    if source.read(1):
        raise ValueError("architecture project has trailing input")
    return sources, modules


def _runtime_loaders(resolver, requests):
    results = []
    for value in requests:
        _exact(value, ("id", "request"), "runtime loader request")
        result = {"id": value["id"], "error": "", "targets": [], "runtimeType": ""}
        try:
            targets, runtime_type = resolve_loader(resolver, value["request"])
            result.update(targets=targets, runtimeType=runtime_type)
        except (
            ValueError,
            TypeError,
            KeyError,
            IndexError,
            AttributeError,
            RecursionError,
        ) as error:
            result["error"] = str(error)[:4096]
        results.append(result)
    return results


def resolve_stream(source, output):
    record = source.readline(MAX_RECORD_BYTES + 1)
    if len(record) > MAX_RECORD_BYTES or not record.endswith(b"\n"):
        raise ValueError("architecture project header exceeds its transport boundary")
    header = json.loads(record, object_pairs_hook=_unique_object)
    _exact(
        header,
        ("protocol", "count", "root", "registries", "runtimeChecks", "runtimeLoaders"),
        "architecture project header",
    )
    if (
        header["protocol"] != "python-architecture-project/v1"
        or type(header["count"]) is not int
        or not 0 <= header["count"] <= MAX_MODULES
        or not isinstance(header["runtimeChecks"], list)
        or not isinstance(header["runtimeLoaders"], list)
    ):
        raise ValueError("architecture project header is invalid")
    sources, modules = _sources(source, header["count"])
    facts = _Resolver(modules)
    runtime_resolver = RuntimeCheckResolver(modules, facts)
    evidence, problems = resolve_runtime_checks(
        modules, header["runtimeChecks"], runtime_resolver
    )
    result = {
        "protocol": header["protocol"],
        "covered": [
            {"path": item["path"], "sourceSha256": item["sourceSha256"]}
            for item in modules
        ],
        "sources": sources,
        "reads": typed_dict_reads(modules, facts),
        "imports": module_imports(modules, facts),
        "objectImports": ObjectImportResolver(
            modules, header["root"], header["registries"], facts
        ).imports(),
        "runtimeChecks": {"evidence": evidence, "problems": problems},
        "runtimeLoaders": _runtime_loaders(runtime_resolver, header["runtimeLoaders"]),
    }
    encoded = json.dumps(
        result, ensure_ascii=False, sort_keys=True, separators=(",", ":")
    ).encode("utf-8")
    if len(encoded) > MAX_FACT_BYTES + 4 * 16 * 1024 * 1024:
        raise ValueError("architecture project output exceeds its response boundary")
    output.write(encoded)


if __name__ == "__main__":
    try:
        resolve_stream(sys.stdin.buffer, sys.stdout.buffer)
    except (ValueError, TypeError) as error:
        sys.stderr.write(str(error) + "\n")
        raise SystemExit(2) from None
