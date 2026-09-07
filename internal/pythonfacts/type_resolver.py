import keyword
import re

ROOTS = {"typing.TypedDict", "typing_extensions.TypedDict"}
MAX_DEPTH = 128
MAX_FACTS = 2000000
MAX_MODULES = 65536
MAX_FACT_BYTES = 256 * 1024 * 1024
MAX_RECORD_BYTES = 16 * 1024 * 1024
MAX_OUTPUT_BYTES = 16 * 1024 * 1024


def _exact(value, keys, label):
    if not isinstance(value, dict) or set(value) != set(keys):
        raise ValueError(f"{label} has missing or unexpected fields")


def _name(value):
    return (
        isinstance(value, str)
        and bool(value)
        and all(
            part.isidentifier() and not keyword.iskeyword(part)
            for part in value.split(".")
        )
    )


def _site(value):
    _exact(value, ("line", "column"), "type fact source location")
    if any(type(value[key]) is not int or value[key] < 1 for key in value):
        raise ValueError("type fact source location is invalid")


def _expression(value, depth=0):
    _exact(value, ("kind", "name", "args"), "type expression")
    if (
        depth >= MAX_DEPTH
        or not isinstance(value["name"], str)
        or not isinstance(value["args"], list)
    ):
        raise ValueError("type expression is invalid or exceeds its depth limit")
    kind = value["kind"]
    if kind not in {
        "name",
        "call",
        "subscript",
        "union",
        "string",
        "literal",
        "unknown",
        "list",
        "ellipsis",
    }:
        raise ValueError("type expression kind is invalid")
    if kind not in {"subscript", "union", "list"} and value["args"]:
        raise ValueError("type expression has unexpected arguments")
    for argument in value["args"]:
        _expression(argument, depth + 1)


def _span(start, end):
    _site(start)
    _site(end)
    if (end["line"], end["column"]) < (start["line"], start["column"]):
        raise ValueError("call or binding source span is reversed")


def _call_argument(value, depth=0):
    _exact(
        value, ("kind", "value", "text", "site", "endSite", "children"), "call argument"
    )
    if depth >= 64 or value["kind"] not in {
        "expression",
        "string",
        "name",
        "call",
        "starred",
        "subscript",
        "attribute",
        "integer",
        "choice",
        "collection",
    }:
        raise ValueError("call argument kind is invalid")
    for key in ("value", "text"):
        if not isinstance(value[key], str) or len(value[key].encode("utf-8")) > 65536:
            raise ValueError("call argument text is invalid or exceeds its boundary")
    if not value["text"]:
        raise ValueError("call argument omits its expression")
    if value["kind"] == "name" and not _name(value["value"]):
        raise ValueError("call argument name is invalid")
    if value["kind"] in {"expression", "starred"} and value["value"]:
        raise ValueError("call argument has unexpected literal evidence")
    _span(value["site"], value["endSite"])
    _call_children(value, depth)


def _call_children(value, depth):
    expected = {"subscript": 2, "attribute": 1, "choice": 2}.get(value["kind"], 0)
    if not isinstance(value["children"], list) or (
        value["kind"] != "collection" and len(value["children"]) != expected
    ):
        raise ValueError("consumer expression has invalid child evidence")
    for child in value["children"]:
        _contained_argument(child, value, depth + 1)


def _call(value, scopes):
    _exact(
        value,
        (
            "scope",
            "site",
            "endSite",
            "callee",
            "arguments",
            "keywords",
            "conditional",
            "statement",
            "guard",
            "direct",
            "shape",
            "typeGuards",
            "flow",
        ),
        "consumer call",
    )
    if value["scope"] not in scopes or type(value["conditional"]) is not bool:
        raise ValueError("consumer call has invalid scope or conditional metadata")
    _statement(value)
    _call_import_metadata(value, scopes)
    _flow(value["flow"])
    if type(value["guard"]) is not bool or type(value["direct"]) is not bool:
        raise ValueError("consumer call has invalid runtime-check metadata")
    _span(value["site"], value["endSite"])
    _contained_argument(value["callee"], value)
    if not isinstance(value["arguments"], list) or not isinstance(
        value["keywords"], list
    ):
        raise TypeError("consumer call omits argument collections")
    for argument in value["arguments"]:
        _contained_argument(argument, value)
    names = set()
    for argument in value["keywords"]:
        _exact(argument, ("name", "value"), "call keyword")
        name = argument["name"]
        if name is not None:
            if not _name(name) or "." in name or name in names:
                raise ValueError("call keyword name is invalid or duplicated")
            names.add(name)
        _contained_argument(argument["value"], value)


def _call_import_metadata(value, scopes):
    if (
        not isinstance(value["shape"], str)
        or len(value["shape"].encode("utf-8")) > 16384
    ):
        raise ValueError("consumer call has invalid bounded AST shape")
    if (
        not isinstance(value["typeGuards"], list)
        or len(value["typeGuards"]) > MAX_DEPTH
    ):
        raise ValueError("consumer call has invalid type-only guards")
    for guard in value["typeGuards"]:
        _exact(guard, ("scope", "name", "site"), "type-only guard")
        if guard["scope"] not in scopes or not _name(guard["name"]):
            raise ValueError("type-only guard has invalid lexical evidence")
        _site(guard["site"])


def _flow(value):
    if (
        not isinstance(value, list)
        or len(value) > MAX_DEPTH
        or any(
            not isinstance(item, str) or not item or len(item) > 128 for item in value
        )
    ):
        raise ValueError("source control flow exceeds its evidence boundary")


def _contained_argument(argument, call, depth=0):
    _call_argument(argument, depth)
    for position, lower in (("site", True), ("endSite", False)):
        actual = (argument[position]["line"], argument[position]["column"])
        bound = (call[position]["line"], call[position]["column"])
        if (lower and actual < bound) or (not lower and actual > bound):
            raise ValueError("call argument is outside its consumer source span")


def _calls(values, scopes):
    if not isinstance(values, list):
        raise TypeError("source facts omit consumer call collection")
    seen = set()
    for value in values:
        _call(value, scopes)
        identity = (
            value["site"]["line"],
            value["site"]["column"],
            value["endSite"]["line"],
            value["endSite"]["column"],
        )
        if identity in seen:
            raise ValueError("source facts duplicate a consumer call")
        seen.add(identity)


def _field_error(module, binding, field, reason):
    site = field["site"]
    return ValueError(
        f"{module['path']}:{site['line']}:{site['column']}: "
        f"TypedDict {binding['name']}.{field['name']}: {reason}"
    )


def _fields(fields):
    if not isinstance(fields, list):
        raise TypeError("TypedDict fields are not an array")
    for field in fields:
        _exact(field, ("name", "site", "type"), "TypedDict field")
        if not isinstance(field["name"], str):
            raise TypeError("TypedDict field name is invalid")
        _site(field["site"])
        _expression(field["type"])


def _binding(value, scopes):
    _exact(
        value,
        (
            "scope",
            "name",
            "kind",
            "site",
            "endSite",
            "definitionLine",
            "activationSite",
            "valueSite",
            "valueEndSite",
            "conditional",
            "flow",
            "statement",
            "decorators",
            "runtimeClass",
            "validator",
            "objectPredicate",
            "reference",
            "annotationScope",
            "annotation",
            "value",
            "bases",
            "fields",
            "classValid",
            "factory",
        ),
        "type binding",
    )
    if value["scope"] not in scopes or value["annotationScope"] not in scopes:
        raise ValueError("type binding has no lexical scope")
    if value["name"] != "*" and (not _name(value["name"]) or "." in value["name"]):
        raise ValueError("type binding name is invalid")
    if value["kind"] not in {
        "unknown",
        "import",
        "alias",
        "annotated",
        "parameter",
        "class",
        "function",
    }:
        raise ValueError("type binding kind is invalid")
    _binding_details(value)


def _binding_details(value):
    _runtime_binding(value)
    _flow(value["flow"])
    _span(value["site"], value["activationSite"])
    if (
        type(value["conditional"]) is not bool
        or type(value["classValid"]) is not bool
        or not isinstance(value["reference"], str)
    ):
        raise ValueError("type binding metadata is invalid")
    _span(value["site"], value["endSite"])
    if (
        type(value["definitionLine"]) is not int
        or not 1 <= value["definitionLine"] <= value["site"]["line"]
    ):
        raise ValueError("binding definition location is invalid")
    if (value["valueSite"] is None) != (value["valueEndSite"] is None):
        raise ValueError("binding value omits part of its source span")
    if value["valueSite"] is not None:
        _span(value["valueSite"], value["valueEndSite"])
    _expression(value["annotation"])
    _expression(value["value"])
    if not isinstance(value["bases"], list):
        raise TypeError("type bases are not an array")
    for base in value["bases"]:
        _expression(base)
    _fields(value["fields"])
    factory = value["factory"]
    _exact(factory, ("name", "fields", "valid"), "TypedDict factory")
    if not isinstance(factory["name"], str) or type(factory["valid"]) is not bool:
        raise ValueError("TypedDict factory metadata is invalid")
    _fields(factory["fields"])


def _statement(value):
    if type(value["statement"]) is not int or value["statement"] < -1:
        raise ValueError("source statement position is invalid")


def _runtime_binding(value):
    _statement(value)
    if type(value["runtimeClass"]) is not bool or not isinstance(
        value["decorators"], list
    ):
        raise ValueError("runtime class metadata is invalid")
    for decorator in value["decorators"]:
        _expression(decorator)
    if value["validator"] is not None:
        _span(value["site"], value["validator"])
        _span(value["validator"], value["endSite"])
    if value["objectPredicate"] is not None:
        _object_predicate(value["objectPredicate"])


def _object_predicate(value):
    _exact(value, ("parameter", "namespace", "references"), "object predicate")
    if not _name(value["parameter"]) or "." in value["parameter"]:
        raise ValueError("object predicate parameter is invalid")
    if not _name(value["namespace"]):
        raise ValueError("object predicate namespace is invalid")
    _exact(
        value["references"],
        ("type", "str", "normalize", "all", "keyword"),
        "object predicate references",
    )
    if not all(_name(reference) for reference in value["references"].values()):
        raise ValueError("object predicate reference is invalid")


def _scopes(values):
    if not isinstance(values, list) or not values:
        raise ValueError("type facts omit lexical scopes")
    result = {}
    for value in values:
        _exact(value, ("id", "parent", "kind"), "type scope")
        if not isinstance(value["id"], str) or not value["id"] or value["id"] in result:
            raise ValueError("type scope identity is invalid or duplicated")
        if value["kind"] not in {"module", "function", "class", "comprehension"}:
            raise ValueError("type scope kind is invalid")
        if result and value["parent"] not in result:
            raise ValueError("type scope parent is missing or reordered")
        if not result and value != {"id": "module", "parent": "", "kind": "module"}:
            raise ValueError("type facts have no exact module scope")
        result[value["id"]] = value
    return result


def _module(value):
    _exact(value, ("path", "module", "package", "sourceSha256", "facts"), "type module")
    path = value["path"]
    if (
        not isinstance(path, str)
        or not path.endswith((".py", ".pyi"))
        or "\\" in path
        or any(part in {"", ".", ".."} for part in path.split("/"))
    ):
        raise ValueError("type module path is invalid")
    if any(
        not isinstance(value[key], str) or (value[key] and not _name(value[key]))
        for key in ("module", "package")
    ):
        raise ValueError("type module name is invalid")
    if not isinstance(value["sourceSha256"], str) or not re.fullmatch(
        "[0-9a-f]{64}", value["sourceSha256"]
    ):
        raise ValueError("type module has no source identity")
    return _module_facts(value)


def _module_facts(value):
    facts = value["facts"]
    _exact(facts, ("scopes", "bindings", "reads", "calls"), "source type facts")
    scopes = _scopes(facts["scopes"])
    if not isinstance(facts["bindings"], list) or not isinstance(facts["reads"], list):
        raise TypeError("source type facts omit bindings or reads")
    bindings = {}
    for binding in facts["bindings"]:
        _binding(binding, scopes)
        bindings.setdefault((binding["scope"], binding["name"]), []).append(binding)
    for read in facts["reads"]:
        _exact(read, ("scope", "site", "receiver", "key"), "literal-key read")
        if read["scope"] not in scopes or not isinstance(read["key"], str):
            raise ValueError("literal-key read has invalid scope or key")
        _site(read["site"])
        _expression(read["receiver"])
    _calls(facts["calls"], scopes)
    return {**value, "scopes": scopes, "bindings": bindings}


class _Resolver:
    def __init__(self, modules):
        self.files = {}
        self.modules = {}
        self.module_roots = set()
        self.types = {}
        self.resolving = set()
        count = 0
        if len(modules) > MAX_MODULES:
            raise ValueError("type project exceeds its source count limit")
        for source in modules:
            module = _module(source)
            if module["path"] in self.files:
                raise ValueError("type project duplicates a source")
            self.files[module["path"]] = module
            self.modules.setdefault(module["module"], []).append(module)
            if module["module"]:
                self.module_roots.add(module["module"].split(".")[0])
            count += sum(
                len(module["facts"][key])
                for key in ("scopes", "bindings", "reads", "calls")
            )
            count += sum(
                len(call["arguments"]) + len(call["keywords"]) + 1
                for call in module["facts"]["calls"]
            )
            if count > MAX_FACTS:
                raise ValueError("type project exceeds its compact fact count limit")

    def binding(self, module, scope, name):
        while scope:
            if (scope, "*") in module["bindings"]:
                return None
            values = module["bindings"].get((scope, name))
            if values is not None:
                return (
                    values[0]
                    if len(values) == 1 and not values[0]["conditional"]
                    else None
                )
            scope = module["scopes"][scope]["parent"]
        return None

    def reference(self, module, scope, name, seen=frozenset()):
        if not _name(name):
            return None
        identity = module["path"], scope, name
        if identity in seen or len(seen) >= MAX_DEPTH:
            raise ValueError(
                "Python type alias or re-export is cyclic or exceeds resolution depth"
            )
        seen = seen | {identity}
        root, _, suffix = name.partition(".")
        binding = self.binding(module, scope, root)
        if binding is None:
            return None
        if binding["kind"] == "import":
            target = self.import_reference(module, binding["reference"])
            return self.import_target(target + ("." + suffix if suffix else ""), seen)
        if binding["kind"] == "alias" and binding["value"]["kind"] == "name":
            target = binding["value"]["name"] + ("." + suffix if suffix else "")
            return self.reference(module, binding["scope"], target, seen)
        return (module["path"], binding) if not suffix else None

    def import_reference(self, module, reference):
        level = len(reference) - len(reference.lstrip("."))
        if not level:
            return reference
        package = module["package"].split(".") if module["package"] else []
        if level > len(package):
            raise ValueError("Python type re-export escapes its package")
        return ".".join([*package[: len(package) - level + 1], reference[level:]])

    def import_target(self, target, seen):
        parts = target.split(".")
        for index in range(len(parts), 0, -1):
            name = ".".join(parts[:index])
            if name not in self.modules:
                continue
            candidates = self.modules[name]
            if len(candidates) != 1:
                raise ValueError("Python type import has ambiguous module definitions")
            if index == len(parts):
                return None
            return self.reference(
                candidates[0], "module", ".".join(parts[index:]), seen
            )
        if parts[0] in self.module_roots:
            return None
        return target

    def definition(self, reference):
        if reference is None or isinstance(reference, str):
            return None
        path, binding = reference
        identity = (
            path,
            binding["scope"],
            binding["name"],
            binding["site"]["line"],
            binding["site"]["column"],
        )
        if identity in self.types:
            return self.types[identity]
        if identity in self.resolving or len(self.resolving) >= MAX_DEPTH:
            raise ValueError(
                "TypedDict inheritance is cyclic or exceeds resolution depth"
            )
        self.resolving.add(identity)
        result = self.resolve_definition(self.files[path], binding)
        self.resolving.remove(identity)
        self.types[identity] = result
        return result

    def resolve_definition(self, module, binding):
        if binding["kind"] == "class":
            inherited = [
                self.base(module, binding["scope"], base) for base in binding["bases"]
            ]
            if not any(value is not None for value in inherited):
                return None
            if not binding["classValid"] or any(value is None for value in inherited):
                raise ValueError(
                    "TypedDict has unsupported class, base, or field expressions"
                )
            fields = binding["fields"]
        elif binding["kind"] == "alias" and binding["value"]["kind"] == "call":
            reference = self.reference(
                module, binding["scope"], binding["value"]["name"]
            )
            if not isinstance(reference, str) or reference not in ROOTS:
                return None
            if (
                not binding["factory"]["valid"]
                or binding["factory"]["name"] != binding["name"]
            ):
                raise ValueError(
                    "TypedDict factory has unsupported or ambiguous arguments"
                )
            inherited, fields = [], binding["factory"]["fields"]
        else:
            return None
        self.require_unique_definition(module, binding)
        return self.merge_fields(module, binding, inherited, fields)

    def base(self, module, scope, expression):
        if expression["kind"] != "name":
            return None
        reference = self.reference(module, scope, expression["name"])
        if isinstance(reference, str) and reference in ROOTS:
            return {}
        return self.definition(reference)

    def require_unique_definition(self, module, binding):
        values = module["bindings"].get((binding["scope"], binding["name"]), [])
        if len(values) != 1 or binding["conditional"]:
            raise ValueError("TypedDict definition is duplicated or conditional")

    def merge_fields(self, module, binding, inherited, fields):
        result = {}
        for parent in inherited:
            for name, field in parent.items():
                if name in result and result[name] != field:
                    raise _field_error(
                        module,
                        binding,
                        {"name": name, "site": binding["site"]},
                        "inheritance has ambiguous duplicate keys",
                    )
                result[name] = field
        for field in fields:
            if field["name"] in result:
                raise _field_error(module, binding, field, "duplicate keys")
            if not self.field_type(module, binding["scope"], field["type"]):
                raise _field_error(
                    module, binding, field, "unsupported field type expression"
                )
            result[field["name"]] = {
                "path": module["path"],
                "typeScope": binding["scope"],
                "typeName": binding["name"],
                "key": field["name"],
                "line": field["site"]["line"],
                "column": field["site"]["column"],
            }
        return result

    def field_type(self, module, scope, expression):
        kind = expression["kind"]
        if kind in {"name", "string", "literal"}:
            return True
        if kind == "subscript":
            reference = self.reference(module, scope, expression["name"])
            if isinstance(reference, str) and reference in {
                "typing.Callable",
                "collections.abc.Callable",
            }:
                return self.callable_type(module, scope, expression["args"])
        return kind in {"subscript", "union"} and all(
            self.field_type(module, scope, value) for value in expression["args"]
        )

    def callable_type(self, module, scope, arguments):
        if len(arguments) != 2:
            return False
        parameters, result = arguments
        if not self.field_type(module, scope, result):
            return False
        if parameters["kind"] == "ellipsis":
            return True
        if parameters["kind"] == "list":
            return all(
                self.field_type(module, scope, value) for value in parameters["args"]
            )
        return self.field_type(module, scope, parameters)

    def receiver(self, module, scope, expression, site, seen=frozenset()):
        if len(seen) >= MAX_DEPTH:
            raise ValueError("TypedDict receiver alias exceeds resolution depth")
        if expression["kind"] == "call":
            return self.definition(self.reference(module, scope, expression["name"]))
        if expression["kind"] != "name" or "." in expression["name"]:
            return None
        binding = self.binding(module, scope, expression["name"])
        if binding is None:
            return None
        identity = module["path"], binding["scope"], binding["name"]
        if identity in seen or self.late_local_binding(scope, binding, site):
            return None
        if binding["kind"] in {"annotated", "parameter"}:
            annotation = binding["annotation"]
            if annotation["kind"] not in {"name", "string"}:
                return None
            return self.definition(
                self.reference(module, binding["annotationScope"], annotation["name"])
            )
        if binding["kind"] == "alias":
            return self.receiver(
                module,
                binding["scope"],
                binding["value"],
                binding["site"],
                seen | {identity},
            )
        return None

    def late_local_binding(self, scope, binding, site):
        if binding["kind"] == "parameter" or scope != binding["scope"]:
            return False
        return (binding["site"]["line"], binding["site"]["column"]) >= (
            site["line"],
            site["column"],
        )

    def resolve_definitions(self):
        for module in self.files.values():
            for binding in module["facts"]["bindings"]:
                if binding["kind"] in {"class", "alias"}:
                    self.definition((module["path"], binding))

    def fields(self):
        self.resolve_definitions()
        fields = {
            (
                field["path"],
                field["typeScope"],
                field["typeName"],
                field["key"],
                field["line"],
                field["column"],
            ): field
            for resolved in self.types.values()
            if resolved is not None
            for field in resolved.values()
        }
        return [fields[key] for key in sorted(fields)]

    def run(self):
        self.resolve_definitions()
        reads = []
        for module in self.files.values():
            for read in module["facts"]["reads"]:
                fields = self.receiver(
                    module, read["scope"], read["receiver"], read["site"]
                )
                if fields is not None and read["key"] in fields:
                    reads.append(
                        {
                            "path": module["path"],
                            "line": read["site"]["line"],
                            "column": read["site"]["column"],
                            "field": fields[read["key"]],
                        }
                    )
        reads.sort(
            key=lambda value: (
                value["path"],
                value["line"],
                value["column"],
                value["field"]["path"],
                value["field"]["line"],
            )
        )
        return reads


def typed_dict_reads(modules, facts=None, *, fields=False):
    resolver = facts or _Resolver(modules)
    return resolver.fields() if fields else resolver.run()


def _unique_object(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError("type protocol has a duplicate JSON key")
        result[key] = value
    return result
