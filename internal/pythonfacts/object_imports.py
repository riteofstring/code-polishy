import hashlib
import json
import posixpath
import sys

from loader_bindings import LoaderBindings
from object_guards import object_guard
from reachability import (
    ReachabilityResolver,
    _module_object,
    _path,
    _registry_targets,
)
from type_resolver import (
    MAX_FACT_BYTES,
    MAX_MODULES,
    MAX_OUTPUT_BYTES,
    MAX_RECORD_BYTES,
    _exact,
    _unique_object,
)

LOADER = "pkgutil.resolve_name"


class ObjectImportResolver(ReachabilityResolver):
    def __init__(self, modules, root, registries):
        super().__init__(modules)
        if root != ".":
            _path(root)
        self.root = root
        self.loader_bindings = LoaderBindings(self.facts)
        self.guard_calls = {}
        self.registries = {}
        for registry in registries:
            _exact(registry, ("path", "content", "error"), "object import registry")
            _path(registry["path"])
            if registry["path"] in self.registries:
                raise ValueError("object import registry is duplicated")
            if not all(isinstance(registry[key], str) for key in ("content", "error")):
                raise TypeError("object import registry has invalid input")
            self.registries[registry["path"]] = registry

    def imports(self):
        result = []
        for path, module in sorted(self.facts.files.items()):
            for call in module["facts"]["calls"]:
                callee = call["callee"]
                if callee[
                    "kind"
                ] != "name" or LOADER not in self.loader_bindings.candidates(
                    module, call["scope"], callee["value"]
                ):
                    continue
                result.append(self.import_fact(path, module, call))
        return result

    def import_fact(self, path, module, call):
        fact = {
            "path": path,
            "callable": self.callable_name(module, call["scope"]),
            "site": call["site"],
            "endSite": call["endSite"],
            "argument": call["arguments"][0]["text"] if call["arguments"] else "",
            "targets": [],
            "registry": None,
            "guard": None,
            "error": "",
        }
        try:
            if self.callee(module, call) != LOADER:
                raise ValueError("object loader binding is ambiguous or wildcard")
            if len(call["arguments"]) != 1 or call["keywords"]:
                raise ValueError("object loader requires one positional argument")
            argument = call["arguments"][0]
            if argument["kind"] == "string":
                names = [_module_object(argument["value"])]
            elif argument["kind"] == "name":
                fact["guard"] = object_guard(self, module, call)
                names = []
            else:
                names, fact["registry"] = self.registry_targets(module, call)
            fact["targets"] = [
                {
                    "module": module_name,
                    "symbol": symbol,
                    "standardLibrary": module_name.partition(".")[0]
                    in sys.stdlib_module_names,
                }
                for module_name, symbol in sorted(set(names))
            ]
        except (ValueError, TypeError, KeyError, IndexError, RecursionError) as error:
            fact["error"] = str(error)[:4096]
        return fact

    def registry_targets(self, module, call):
        path, pointer, dynamic = self.registry_selection(module, call)
        path = posixpath.join(self.root, path) if self.root != "." else path
        registry = self.registries.get(path)
        if registry is None or registry["error"]:
            raise ValueError("object loader has no current governed registry input")
        names = _registry_targets(registry["content"], pointer, dynamic)
        return names, {
            "path": path,
            "jsonPointer": pointer,
            "sha256": hashlib.sha256(registry["content"].encode("utf-8")).hexdigest(),
        }


def resolve_stream(source, output):
    record = source.readline(MAX_RECORD_BYTES + 1)
    if len(record) > MAX_RECORD_BYTES or not record.endswith(b"\n"):
        raise ValueError("object import header exceeds its transport boundary")
    header = json.loads(record, object_pairs_hook=_unique_object)
    _exact(header, ("protocol", "count", "root", "registries"), "object import header")
    if (
        header["protocol"] != "python-object-import-project/v1"
        or type(header["count"]) is not int
        or not 0 <= header["count"] <= MAX_MODULES
        or not isinstance(header["registries"], list)
    ):
        raise ValueError("object import header is invalid")
    modules, size = [], 0
    for _ in range(header["count"]):
        record = source.readline(MAX_RECORD_BYTES + 1)
        size += len(record)
        if (
            len(record) > MAX_RECORD_BYTES
            or size > MAX_FACT_BYTES
            or not record.endswith(b"\n")
        ):
            raise ValueError("object import project exceeds its transport boundary")
        modules.append(json.loads(record, object_pairs_hook=_unique_object))
    if source.read(1):
        raise ValueError("object import project has trailing input")
    result = {
        "protocol": header["protocol"],
        "covered": [
            {"path": value["path"], "sourceSha256": value["sourceSha256"]}
            for value in modules
        ],
        "imports": ObjectImportResolver(
            modules, header["root"], header["registries"]
        ).imports(),
    }
    encoded = json.dumps(
        result, ensure_ascii=False, sort_keys=True, separators=(",", ":")
    ).encode("utf-8")
    if len(encoded) > MAX_OUTPUT_BYTES:
        raise ValueError("object import output exceeds its response boundary")
    output.write(encoded)


if __name__ == "__main__":
    resolve_stream(sys.stdin.buffer, sys.stdout.buffer)
