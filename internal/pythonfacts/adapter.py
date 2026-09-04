import importlib.metadata
import json
import sys
import tokenize
import tomllib
from importlib import import_module

from packaging.markers import Op, Value, Variable
from packaging.metadata import Metadata
from packaging.requirements import Requirement
from packaging.specifiers import InvalidSpecifier, SpecifierSet
from packaging.utils import canonicalize_name
from packaging.version import InvalidVersion, Version

from source_adapter import _source

PROTOCOL = "python-facts/v1"
PACKAGING_VERSION = "26.3"
MAX_INPUT_BYTES = 8 * 1024 * 1024
MAX_OUTPUT_BYTES = 16 * 1024 * 1024
MAX_ITEMS = 4096
MAX_SOURCE_BYTES = 2 * 1024 * 1024
EXACT_MARKER_VARIABLES = {
    "implementation_name",
    "os_name",
    "platform_machine",
    "platform_python_implementation",
    "platform_system",
    "sys_platform",
}
FACT_ERRORS = (
    ValueError,
    TypeError,
    SyntaxError,
    LookupError,
    tokenize.TokenError,
    RecursionError,
    ExceptionGroup,
)


def _object(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError(f"duplicate JSON key {key!r}")
        result[key] = value
    return result


def _exact_object(value, keys, name):
    if not isinstance(value, dict) or set(value) != set(keys):
        raise ValueError(f"{name} has invalid fields")


def _line(source, position):
    return source.count("\n", 0, position) + 1


def _value_line(value, source, start, end, cursor):
    if not isinstance(value, str) or not value:
        return _line(source, start)
    position = source.find(value, cursor[0], end)
    if position < 0:
        position = source.find(value, start, end)
    if position < 0:
        prefix = value.splitlines()[0][:16]
        if prefix:
            position = source.find(prefix, cursor[0], end)
            if position < 0:
                position = source.find(prefix, start, end)
    if position < 0:
        return _line(source, start)
    cursor[0] = position + len(value)
    return _line(source, position)


def _encode_value(value, source, start, end, cursor):
    line = _value_line(value, source, start, end, cursor)
    if isinstance(value, str):
        return {
            "kind": "string",
            "text": value,
            "line": line,
            "array": [],
            "inline": [],
        }
    if isinstance(value, list):
        return {
            "kind": "array",
            "text": "",
            "line": line,
            "array": [
                _encode_value(item, source, start, end, cursor) for item in value
            ],
            "inline": [],
        }
    if isinstance(value, dict):
        return {
            "kind": "inline",
            "text": "",
            "line": line,
            "array": [],
            "inline": [
                {
                    "path": [key],
                    "value": _encode_value(item, source, start, end, cursor),
                }
                for key, item in value.items()
            ],
        }
    if isinstance(value, bool):
        text = "true" if value else "false"
    elif isinstance(value, int):
        text = str(value)
    elif isinstance(value, float):
        text = repr(value).lower()
    else:
        text = str(value)
    return {"kind": "other", "text": text, "line": line, "array": [], "inline": []}


def _string_values(value):
    if value["kind"] == "string":
        yield value["text"]
    for item in value["array"]:
        yield from _string_values(item)
    for item in value["inline"]:
        yield from _string_values(item["value"])


def _marker_equality(marker):
    if marker is None:
        return "", ""
    markers = marker._markers
    if len(markers) != 1 or not isinstance(markers[0], tuple):
        return "", ""
    left, operator, right = markers[0]
    if not isinstance(operator, Op) or operator.value != "==":
        return "", ""
    if isinstance(left, Variable) and isinstance(right, Value):
        variable, value = left.value, right.value
    elif isinstance(left, Value) and isinstance(right, Variable):
        variable, value = right.value, left.value
    else:
        return "", ""
    if variable not in EXACT_MARKER_VARIABLES:
        return "", ""
    return variable, value


def _requirement(value):
    try:
        parsed = Requirement(value)
        specifiers = sorted(
            (
                {"operator": item.operator, "version": item.version}
                for item in parsed.specifier
            ),
            key=lambda item: (item["operator"], item["version"]),
        )
        marker_variable, marker_value = _marker_equality(parsed.marker)
        return {
            "input": value,
            "name": canonicalize_name(parsed.name),
            "extras": sorted(canonicalize_name(extra) for extra in parsed.extras),
            "specifier": str(parsed.specifier),
            "specifiers": specifiers,
            "marker": str(parsed.marker) if parsed.marker is not None else "",
            "markerVariable": marker_variable,
            "markerValue": marker_value,
            "url": parsed.url or "",
            "error": "",
        }
    except (ValueError, TypeError) as error:
        return {
            "input": value,
            "name": "",
            "extras": [],
            "specifier": "",
            "specifiers": [],
            "marker": "",
            "markerVariable": "",
            "markerValue": "",
            "url": "",
            "error": str(error),
        }


def _ruff_target(value):
    try:
        specifiers = SpecifierSet(value)
    except InvalidSpecifier as error:
        return {"input": value, "target": "", "error": str(error)}
    candidates = {0, 1, 2, 100, 1000000}
    lower = None
    for specifier in specifiers:
        parsed = _specifier_version(specifier.version)
        if parsed is None:
            continue
        if not _stable_public_version(parsed):
            return _ruff_error(
                value, "specifier must use stable public Python releases"
            )
        lower = _ruff_lower_bound(lower, specifier.operator, parsed)
        candidates.update(_version_patches(parsed))
    if lower is None or len(lower.release) < 2:
        return _ruff_error(value, "specifier has no minimum supported Python version")
    if lower.release[:2] < (3, 7) or lower.release[:2] > (3, 12):
        message = (
            "minimum Python version is outside the carried parser range "
            "of 3.7 through 3.12"
        )
        return _ruff_error(value, message)
    target = _supported_ruff_target(specifiers, lower, candidates)
    if target:
        return {"input": value, "target": target, "error": ""}
    message = (
        "specifier has no stable Python version supported by the carried "
        "CPython 3.12 parser"
    )
    return _ruff_error(value, message)


def _ruff_error(value, message):
    return {"input": value, "target": "", "error": message}


def _specifier_version(value):
    try:
        return Version(value.removesuffix(".*"))
    except InvalidVersion:
        return None


def _stable_public_version(version):
    return (
        not version.is_prerelease
        and not version.is_devrelease
        and version.local is None
    )


def _ruff_lower_bound(lower, operator, version):
    if operator not in {">=", ">", "~=", "=="}:
        return lower
    if lower is None or version > lower:
        return version
    return lower


def _version_patches(version):
    if len(version.release) < 3:
        return set()
    patch = version.release[2]
    return {max(0, patch - 1), patch, patch + 1}


def _supported_ruff_target(specifiers, lower, patches):
    for minor in range(lower.release[1], 13):
        for patch in sorted(patches):
            candidate = Version(f"3.{minor}.{patch}")
            if specifiers.contains(candidate, prereleases=False):
                return f"py3{minor}"
    return ""


def _manifest(path, source):
    captures = []
    tables = []
    array_tables = set()
    parser = import_module("tomllib._parser")
    key_value_name = "key_value_rule"
    dict_name = "create_dict_rule"
    list_name = "create_list_rule"
    original_key_value = getattr(parser, key_value_name)
    original_dict = getattr(parser, dict_name)
    original_list = getattr(parser, list_name)

    def key_value_rule(text, position, output, header, parse_float):
        _, key, value = parser.parse_key_value_pair(text, position, parse_float)
        end = original_key_value(text, position, output, header, parse_float)
        captures.append(
            (
                tuple(header),
                tuple(key),
                value,
                position,
                end,
                tuple(header) in array_tables,
            )
        )
        return end

    def create_dict_rule(text, position, output):
        end, key = original_dict(text, position, output)
        tables.append(
            {"path": list(key), "array": False, "line": _line(text, position)}
        )
        return end, key

    def create_list_rule(text, position, output):
        end, key = original_list(text, position, output)
        array_tables.add(tuple(key))
        tables.append({"path": list(key), "array": True, "line": _line(text, position)})
        return end, key

    setattr(parser, key_value_name, key_value_rule)
    setattr(parser, dict_name, create_dict_rule)
    setattr(parser, list_name, create_list_rule)
    try:
        tomllib.loads(source)
    finally:
        setattr(parser, key_value_name, original_key_value)
        setattr(parser, dict_name, original_dict)
        setattr(parser, list_name, original_list)
    assignments = []
    strings = set()
    for table, key, value, start, end, array_table in captures:
        cursor = [start]
        encoded = _encode_value(value, source, start, end, cursor)
        strings.update(_string_values(encoded))
        assignments.append(
            {
                "tablePath": list(table),
                "keyPath": list(key),
                "arrayTable": array_table,
                "line": _line(source, start),
                "value": encoded,
            }
        )
    requirements = [_requirement(value) for value in sorted(strings)]
    specifiers = [_ruff_target(value) for value in sorted(strings)]
    return {
        "path": path,
        "tables": tables,
        "assignments": assignments,
        "requirements": requirements,
        "specifiers": specifiers,
        "error": "",
    }


def _package_name(value):
    if not isinstance(value, str) or not value or len(value.encode("utf-8")) > 4096:
        raise ValueError("package name is invalid")
    parsed = Requirement(value)
    if (
        parsed.extras
        or parsed.url
        or str(parsed.specifier)
        or parsed.marker is not None
        or parsed.name != value
    ):
        raise ValueError("package name contains requirement syntax")
    return canonicalize_name(parsed.name)


def _lock(path, source):
    document = tomllib.loads(source)
    packages = document.get("package", [])
    if not isinstance(packages, list) or len(packages) > MAX_ITEMS:
        raise ValueError("uv lock has an invalid package count")
    facts = []
    for package in packages:
        if not isinstance(package, dict):
            raise TypeError("uv lock package is not a table")
        name_input = package.get("name")
        version = package.get("version", "")
        source_value = package.get("source", {})
        if not isinstance(version, str) or len(version.encode("utf-8")) > 4096:
            raise ValueError("uv lock package has an invalid version")
        if not isinstance(source_value, dict) or len(source_value) > 32:
            raise ValueError("uv lock package has an invalid source")
        source_fields = []
        for key, value in sorted(source_value.items()):
            if (
                not isinstance(key, str)
                or not key
                or len(key.encode("utf-8")) > 4096
                or not isinstance(value, str)
                or len(value.encode("utf-8")) > 65536
            ):
                raise ValueError("uv lock package source has an invalid field")
            source_fields.append({"name": key, "value": value})
        facts.append(
            {
                "nameInput": name_input,
                "name": _package_name(name_input),
                "version": version,
                "source": source_fields,
            }
        )
    version = document.get("version", 0)
    revision = document.get("revision", 0)
    requires_python = document.get("requires-python", "")
    if (
        type(version) is not int
        or type(revision) is not int
        or not isinstance(requires_python, str)
    ):
        raise ValueError("uv lock has an invalid identity")
    return {
        "path": path,
        "version": version,
        "revision": revision,
        "requiresPython": requires_python,
        "packages": facts,
        "error": "",
    }


def _metadata(path, source):
    parsed = Metadata.from_email(source, validate=True)
    requirements = [_requirement(str(value)) for value in (parsed.requires_dist or [])]
    if len(requirements) > MAX_ITEMS:
        raise ValueError("distribution metadata exceeds the requirement limit")
    name_input = parsed.name
    version = str(parsed.version)
    if (
        not isinstance(name_input, str)
        or not name_input
        or len(name_input.encode("utf-8")) > 4096
    ):
        raise ValueError("distribution metadata has an invalid name")
    if not version or len(version.encode("utf-8")) > 4096:
        raise ValueError("distribution metadata has an invalid version")
    return {
        "path": path,
        "nameInput": name_input,
        "name": canonicalize_name(name_input, validate=True),
        "version": version,
        "requirements": requirements,
        "error": "",
    }


def _item(value, name):
    _exact_object(value, {"path", "source"}, name)
    path = value["path"]
    source = value["source"]
    if not isinstance(path, str) or not path or len(path.encode("utf-8")) > 4096:
        raise ValueError(f"{name} has an invalid path")
    if not isinstance(source, str) or len(source.encode("utf-8")) > MAX_SOURCE_BYTES:
        raise ValueError(f"{name} has invalid source")
    return path, source


def _source_fact(path, source):
    return _source(path, source)


def _analyze(request):
    _validate_request(request)
    manifests = _item_facts(
        request["manifests"],
        "manifest",
        _manifest,
        {"tables": [], "assignments": [], "requirements": [], "specifiers": []},
    )
    locks = _item_facts(
        request["locks"],
        "lock",
        _lock,
        {"version": 0, "revision": 0, "requiresPython": "", "packages": []},
        "packages",
        "uv lock facts exceed the package limit",
    )
    metadata = _item_facts(
        request["metadata"],
        "metadata",
        _metadata,
        {"nameInput": "", "name": "", "version": "", "requirements": []},
        "requirements",
        "distribution metadata facts exceed the requirement limit",
    )
    sources = _item_facts(
        request["sources"],
        "source",
        _source_fact,
        {"sha256": "", "imports": [], "computedImports": []},
    )
    return {
        "protocol": PROTOCOL,
        "pythonVersion": ".".join(str(value) for value in sys.version_info[:3]),
        "packagingVersion": importlib.metadata.version("packaging"),
        "manifests": manifests,
        "locks": locks,
        "metadata": metadata,
        "sources": sources,
        "requirements": _bounded_facts(
            request["requirements"],
            65536,
            "request requirement is invalid",
            _requirement,
        ),
        "specifiers": _bounded_facts(
            request["specifiers"], 65536, "request specifier is invalid", _ruff_target
        ),
        "names": [_name_fact(value) for value in request["names"]],
    }


def _validate_request(request):
    _exact_object(
        request,
        {
            "protocol",
            "operation",
            "manifests",
            "locks",
            "metadata",
            "sources",
            "requirements",
            "specifiers",
            "names",
        },
        "request",
    )
    if request["protocol"] != PROTOCOL or request["operation"] != "analyze":
        raise ValueError("request has an invalid protocol identity")
    for key in (
        "manifests",
        "locks",
        "metadata",
        "sources",
        "requirements",
        "specifiers",
        "names",
    ):
        if not isinstance(request[key], list) or len(request[key]) > MAX_ITEMS:
            raise ValueError(f"request {key} has an invalid count")


def _item_facts(values, label, parser, empty, count_field="", count_error=""):
    result = []
    total = 0
    for index, value in enumerate(values):
        path, source = _item(value, f"{label} {index}")
        try:
            fact = parser(path, source)
            if count_field:
                total += len(fact[count_field])
                if total > MAX_ITEMS:
                    raise ValueError(count_error)
            result.append(fact)
        except FACT_ERRORS as error:
            result.append({"path": path, **empty, "error": str(error)})
    return result


def _bounded_facts(values, limit, message, parser):
    result = []
    for value in values:
        if not isinstance(value, str) or len(value.encode("utf-8")) > limit:
            raise ValueError(message)
        result.append(parser(value))
    return result


def _name_fact(value):
    if not isinstance(value, str) or len(value.encode("utf-8")) > 4096:
        raise ValueError("request package name is invalid")
    try:
        return {"input": value, "normalized": _package_name(value), "error": ""}
    except (ValueError, TypeError) as error:
        return {"input": value, "normalized": "", "error": str(error)}


def _main():
    if sys.version_info[:2] != (3, 12):
        raise ValueError("python-facts requires CPython 3.12")
    if importlib.metadata.version("packaging") != PACKAGING_VERSION:
        raise ValueError(f"python-facts requires packaging {PACKAGING_VERSION}")
    data = sys.stdin.buffer.read(MAX_INPUT_BYTES + 1)
    if len(data) > MAX_INPUT_BYTES:
        raise ValueError("request exceeds the byte limit")
    request = json.loads(data, object_pairs_hook=_object)
    response = (
        json.dumps(
            _analyze(request),
            ensure_ascii=False,
            separators=(",", ":"),
            allow_nan=False,
        ).encode("utf-8")
        + b"\n"
    )
    if len(response) > MAX_OUTPUT_BYTES:
        raise ValueError("response exceeds the byte limit")
    sys.stdout.buffer.write(response)


try:
    _main()
except (ValueError, TypeError, OSError, ImportError, RecursionError) as error:
    sys.stderr.write(str(error) + "\n")
    raise SystemExit(2) from None
