import hashlib
import json
import posixpath
import sys
import unicodedata

from external_contracts import external_consumer, resolve_external_contract
from type_resolver import (
    MAX_FACT_BYTES,
    MAX_MODULES,
    MAX_OUTPUT_BYTES,
    MAX_RECORD_BYTES,
    _exact,
    _name,
    _Resolver,
    _site,
    _unique_object,
)


def _identity(value):
    encoded = json.dumps(
        value, ensure_ascii=True, sort_keys=True, separators=(",", ":")
    ).encode("utf-8")
    return hashlib.sha256(encoded).hexdigest()


def _module_object(value):
    if (
        not isinstance(value, str)
        or value.count(":") != 1
        or unicodedata.normalize("NFKC", value) != value
    ):
        raise ValueError(
            "target must use the normalized python-module-object/v1 grammar"
        )
    module, symbol = value.split(":")
    if not _name(module) or not _name(symbol):
        raise ValueError("target must name one absolute Python module and object")
    return module, symbol


def _path(value):
    if (
        not isinstance(value, str)
        or not value
        or "\\" in value
        or any(part in {"", ".", ".."} for part in value.split("/"))
    ):
        raise ValueError("reachability input path is not canonical and contained")
    if any(character in value for character in "\x00\r\n*?[]{}:"):
        raise ValueError("reachability input path has unsupported characters")


def _declaration(value):
    if not isinstance(value, dict) or value.get("kind") not in {"target", "registry"}:
        raise ValueError("reachability requires a target or registry consumer contract")
    kind = value["kind"]
    _exact(value, ("kind", "project", "consumer", kind), "reachability declaration")
    _path(value["project"])
    if posixpath.basename(value["project"]) != "pyproject.toml":
        raise ValueError("reachability project must name its manifest")
    consumer = value["consumer"]
    if consumer.get("kind") == "callsite":
        consumer = _consumer_declaration(consumer)
    elif kind == "target":
        consumer = external_consumer(consumer)
    else:
        raise ValueError("registry requires an exact callsite consumer")
    if kind == "target":
        _exact(value[kind], ("module", "symbol"), "reachability target")
        _module_object(value[kind]["module"] + ":" + value[kind]["symbol"])
    else:
        _exact(value[kind], ("path", "jsonPointer"), "reachability registry")
        _path(value[kind]["path"])
    return consumer


def _consumer_declaration(consumer):
    _exact(
        consumer,
        (
            "kind",
            "importer",
            "module",
            "callable",
            "site",
            "callee",
            "shape",
            "argument",
            "sourceSha256",
        ),
        "reachability consumer",
    )
    if (
        consumer["kind"] != "callsite"
        or consumer["callee"] != "pkgutil.resolve_name"
        or consumer["shape"] != "module-object-call/v1"
    ):
        raise ValueError("reachability consumer shape is unsupported")
    _path(consumer["importer"])
    _site(consumer["site"])
    if not _name(consumer["module"]) or not _name(consumer["callable"]):
        raise ValueError("reachability consumer names are invalid")
    return consumer


def _scope_for(binding):
    line, column = binding["site"]["line"], binding["site"]["column"]
    return f"{binding['scope']}/{binding['kind']}:{line}:{column}"


def _definition(module, binding):
    return {
        "path": module["path"],
        "line": binding["definitionLine"],
        "end": binding["endSite"]["line"],
        "name": binding["name"],
    }


def _definition_key(value):
    return value["path"], value["line"], value["end"], value["name"]


class ReachabilityResolver:
    def __init__(self, modules):
        self.facts = _Resolver(modules)
        self.identity = _identity(sorted(modules, key=lambda value: value["path"]))

    def runtime_module(self, name):
        modules = self.facts.modules.get(name, [])
        runtime = [module for module in modules if module["path"].endswith(".py")]
        if len(runtime) != 1:
            raise ValueError("target or consumer module is missing or ambiguous")
        return runtime[0]

    def exact_binding(self, module, scope, name):
        values = module["bindings"].get((scope, name), [])
        if (
            len(values) != 1
            or values[0]["conditional"]
            or (scope, "*") in module["bindings"]
        ):
            raise ValueError(
                "target or consumer binding is stale, wildcard, or ambiguous"
            )
        return values[0]

    def callable_scope(self, module, name):
        scope = "module"
        parts = name.split(".")
        for index, part in enumerate(parts):
            binding = self.exact_binding(module, scope, part)
            expected = "function" if index == len(parts) - 1 else "class"
            if binding["kind"] != expected:
                raise ValueError("consumer does not resolve to one containing callable")
            scope = _scope_for(binding)
        return scope

    def callable_name(self, module, scope):
        names = {}
        for bindings in module["bindings"].values():
            for binding in bindings:
                if binding["kind"] in {"function", "class"}:
                    names[_scope_for(binding)] = binding["name"]
        parts = []
        while scope != "module":
            if scope in names:
                parts.append(names[scope])
            elif module["scopes"][scope]["kind"] == "function":
                parts.append("<lambda>")
            scope = scope.rpartition("/")[0]
        return ".".join(reversed(parts)) if parts else "<module>"

    def callee(self, module, call):
        expression = call["callee"]
        if expression["kind"] != "name":
            return None
        return self.facts.reference(module, call["scope"], expression["value"])

    def consumer(self, declaration):
        return self.consumer_binding(_declaration(declaration))

    def consumer_binding(self, expected):
        _consumer_declaration(expected)
        module = self.runtime_module(expected["module"])
        if (
            module["path"] != expected["importer"]
            or module["sourceSha256"] != expected["sourceSha256"]
        ):
            raise ValueError("consumer source path or digest is stale")
        scope = self.callable_scope(module, expected["callable"])
        calls = [
            call
            for call in module["facts"]["calls"]
            if call["site"] == expected["site"]
            and call["scope"] == scope
            and self.callee(module, call) == expected["callee"]
        ]
        if len(calls) != 1:
            raise ValueError("consumer call is moved, shadowed, or ambiguous")
        call = calls[0]
        if (
            len(call["arguments"]) != 1
            or call["keywords"]
            or call["arguments"][0]["text"] != expected["argument"]
        ):
            raise ValueError("consumer call shape or target argument is stale")
        return module, call

    def call_at(self, module, expression):
        calls = [
            call
            for call in module["facts"]["calls"]
            if call["site"] == expression["site"]
            and call["endSite"] == expression["endSite"]
        ]
        if expression["kind"] != "call" or len(calls) != 1:
            raise ValueError("registry expression is not one exact call")
        return calls[0]

    def target(self, module_name, symbol):
        module = self.runtime_module(module_name)
        definitions = self.symbol(module, "module", symbol.split("."), frozenset())
        return {"module": module_name, "symbol": symbol, "definitions": definitions}

    def qualified_target(self, qualified, seen):
        parts = qualified.split(".")
        for index in range(len(parts) - 1, 0, -1):
            module_name = ".".join(parts[:index])
            if module_name in self.facts.modules:
                return self.symbol(
                    self.runtime_module(module_name), "module", parts[index:], seen
                )
        raise ValueError("target re-export is outside the declared project")

    def symbol(self, module, scope, parts, seen):
        identity = module["path"], scope, tuple(parts)
        if identity in seen or len(seen) >= 128:
            raise ValueError(
                "target alias is cyclic or exceeds its resolution boundary"
            )
        seen = seen | {identity}
        binding = self.exact_binding(module, scope, parts[0])
        result = [_definition(module, binding)]
        if binding["kind"] == "import":
            qualified = self.facts.import_reference(module, binding["reference"])
            return result + self.qualified_target(
                ".".join([qualified, *parts[1:]]), seen
            )
        if binding["kind"] == "alias" and binding["value"]["kind"] == "name":
            return result + self.local_alias(
                module, scope, binding["value"]["name"].split(".") + parts[1:], seen
            )
        if len(parts) == 1 and binding["kind"] in {
            "class",
            "function",
            "alias",
            "annotated",
        }:
            return result
        if binding["kind"] != "class":
            raise ValueError("target has no exact class member")
        return result + self.symbol(module, _scope_for(binding), parts[1:], seen)

    def local_alias(self, module, scope, parts, seen):
        binding = self.facts.binding(module, scope, parts[0])
        if binding is None:
            raise ValueError("target alias is missing or ambiguous")
        return self.symbol(module, binding["scope"], parts, seen)

    def registry_path(self, module, expression):
        load = self.call_at(module, expression)
        if (
            self.callee(module, load) != "json.loads"
            or len(load["arguments"]) != 1
            or load["keywords"]
        ):
            raise ValueError("registry consumer must read one exact JSON input")
        read = self.call_at(module, load["arguments"][0])
        callee = read["callee"]
        if (
            callee["kind"] != "attribute"
            or callee["value"] != "read_text"
            or read["arguments"]
        ):
            raise ValueError("registry consumer has no exact text read")
        for argument in read["keywords"]:
            if (
                argument["name"] != "encoding"
                or argument["value"]["kind"] != "string"
                or argument["value"]["value"] != "utf-8"
            ):
                raise ValueError("registry reader requires UTF-8 text")
        path = self.call_at(module, callee["children"][0])
        if (
            self.callee(module, path) != "pathlib.Path"
            or len(path["arguments"]) != 1
            or path["keywords"]
            or path["arguments"][0]["kind"] != "string"
        ):
            raise ValueError("registry consumer has no exact path constructor")
        return path["arguments"][0]["value"]

    def registry_selection(self, module, call):
        expression = call["arguments"][0]
        selectors = []
        while expression["kind"] == "subscript":
            expression, selector = expression["children"]
            selectors.append(selector)
        selectors.reverse()
        dynamic = bool(selectors) and selectors[-1]["kind"] == "name"
        if dynamic:
            binding = self.facts.binding(module, call["scope"], selectors[-1]["value"])
            if (
                binding is None
                or binding["kind"] != "parameter"
                or binding["scope"] != call["scope"]
            ):
                raise ValueError(
                    "registry selector is not one current consumer parameter"
                )
            selectors.pop()
        if any(selector["kind"] not in {"string", "integer"} for selector in selectors):
            raise ValueError("registry selector has unsupported data flow")
        path = self.registry_path(module, expression)
        _path(path)
        pointer = "".join(
            "/" + selector["value"].replace("~", "~0").replace("/", "~1")
            for selector in selectors
        )
        return path, pointer, dynamic

    def registry_selector(self, module, call, declaration):
        path, pointer, dynamic = self.registry_selection(module, call)
        path = posixpath.join(posixpath.dirname(declaration["project"]), path)
        registry = declaration["registry"]
        if path != registry["path"] or pointer != registry["jsonPointer"]:
            raise ValueError(
                "registry input or selector is disconnected from its consumer"
            )
        return dynamic

    def resolve(self, request, inferred):
        declaration = request["declaration"]
        _declaration(declaration)
        external = declaration["consumer"]["kind"] != "callsite"
        if external:
            names = resolve_external_contract(self, request)
        else:
            if request["dependency"] is not None:
                raise ValueError("callsite cannot claim external dependency evidence")
            module, call = self.consumer(declaration)
        if not external and declaration["kind"] == "target":
            argument = call["arguments"][0]
            if argument["kind"] != "string":
                raise ValueError(
                    "target consumer requires one literal module-object argument"
                )
            actual = _module_object(argument["value"])
            target = declaration["target"]
            if actual != (target["module"], target["symbol"]):
                raise ValueError("declared target is disconnected from its consumer")
            names = [actual]
        elif not external:
            dynamic = self.registry_selector(module, call, declaration)
            names = _registry_targets(
                request["registry"], declaration["registry"]["jsonPointer"], dynamic
            )
        targets = [self.target(module, symbol) for module, symbol in sorted(set(names))]
        if any(
            _definition_key(target["definitions"][-1]) in inferred for target in targets
        ):
            raise ValueError("configured target repeats an inferred consumer contract")
        identity = _identity(
            {
                "facts": self.identity,
                "declaration": declaration,
                "registry": request["registry"],
                "dependency": request["dependency"],
                "targets": targets,
            }
        )
        return {
            "id": request["id"],
            "identity": identity,
            "registrySha256": hashlib.sha256(
                request["registry"].encode("utf-8")
            ).hexdigest(),
            "targets": targets,
        }


def _registry_targets(content, pointer, dynamic):
    if (
        not isinstance(content, str)
        or not 0 < len(content.encode("utf-8")) <= 2 * 1024 * 1024
    ):
        raise ValueError("registry exceeds its bounded input contract")
    value = json.loads(
        content, object_pairs_hook=_unique_object, parse_constant=_invalid_constant
    )
    _bounded_registry(value)
    for segment in _pointer_parts(pointer):
        if isinstance(value, dict):
            value = value[segment]
        elif (
            isinstance(value, list)
            and segment.isascii()
            and segment.isdecimal()
            and str(int(segment)) == segment
        ):
            value = value[int(segment)]
        else:
            raise ValueError("registry selector does not resolve exactly")
    if dynamic:
        if isinstance(value, dict):
            value = list(value.values())
        if not isinstance(value, list) or not value:
            raise ValueError(
                "registry parameter must select a nonempty bounded collection"
            )
    else:
        value = [value]
    return [_module_object(target) for target in value]


def _invalid_constant(value):
    raise ValueError(f"registry contains unsupported JSON constant {value}")


def _bounded_registry(value):
    pending, count = [(value, 0)], 0
    while pending:
        current, depth = pending.pop()
        count += 1
        if count > 131072 or depth > 64:
            raise ValueError("registry exceeds its structural resource boundary")
        children = (
            current.values()
            if isinstance(current, dict)
            else current
            if isinstance(current, list)
            else ()
        )
        pending.extend((child, depth + 1) for child in children)


def _pointer_parts(pointer):
    if not isinstance(pointer, str) or (pointer and not pointer.startswith("/")):
        raise ValueError("registry JSON pointer is invalid")
    result = []
    for part in pointer.split("/")[1:] if pointer else ():
        index = 0
        while index < len(part):
            if part[index] == "~":
                index += 1
                if index == len(part) or part[index] not in "01":
                    raise ValueError("registry JSON pointer escape is invalid")
            index += 1
        result.append(part.replace("~1", "/").replace("~0", "~"))
    return result


def resolve_reachability(modules, requests, inferred):
    resolver = ReachabilityResolver(modules)
    evidence, problems = [], []
    consumers = {}
    for request in requests:
        _exact(
            request,
            ("id", "declaration", "registry", "error", "dependency"),
            "reachability input",
        )
        consumer = request["declaration"].get("consumer", {})
        identity = _identity(
            {
                key: consumer.get(key)
                for key in ("importer", "site", "implementation", "member")
            }
        )
        consumers[identity] = consumers.get(identity, 0) + 1
    for request in requests:
        try:
            if request["error"]:
                raise ValueError(request["error"])
            consumer = request["declaration"].get("consumer", {})
            identity = _identity(
                {
                    key: consumer.get(key)
                    for key in ("importer", "site", "implementation", "member")
                }
            )
            if consumers[identity] != 1:
                raise ValueError("consumer has duplicate reachability declarations")
            evidence.append(resolver.resolve(request, inferred))
        except (ValueError, TypeError, KeyError, IndexError, RecursionError) as error:
            problems.append({"id": request["id"], "message": str(error)[:4096]})
    return evidence, problems


def resolve_stream(source, output):
    header = json.loads(
        source.readline(MAX_RECORD_BYTES + 1), object_pairs_hook=_unique_object
    )
    _exact(header, ("protocol", "count", "requests"), "reachability project header")
    if (
        header["protocol"] != "python-reachability-project/v1"
        or type(header["count"]) is not int
        or not 0 <= header["count"] <= MAX_MODULES
        or not isinstance(header["requests"], list)
    ):
        raise ValueError("reachability project header is invalid")
    modules, size = [], 0
    for _ in range(header["count"]):
        record = source.readline(MAX_RECORD_BYTES + 1)
        size += len(record)
        if (
            len(record) > MAX_RECORD_BYTES
            or size > MAX_FACT_BYTES
            or not record.endswith(b"\n")
        ):
            raise ValueError("reachability project exceeds its bounded transport")
        modules.append(json.loads(record, object_pairs_hook=_unique_object))
    if source.read(1):
        raise ValueError("reachability project has trailing input")
    evidence, problems = resolve_reachability(modules, header["requests"], set())
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
        raise ValueError("reachability evidence exceeds its response boundary")
    output.write(encoded)


if __name__ == "__main__":
    resolve_stream(sys.stdin.buffer, sys.stdout.buffer)
