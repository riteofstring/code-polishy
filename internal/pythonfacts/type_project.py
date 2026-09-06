import json
import sys

from module_imports import module_imports
from type_resolver import (
    MAX_FACT_BYTES,
    MAX_MODULES,
    MAX_OUTPUT_BYTES,
    MAX_RECORD_BYTES,
    _exact,
    _unique_object,
    typed_dict_reads,
)


def resolve_stream(source, output):
    header = json.loads(
        source.readline(MAX_RECORD_BYTES + 1), object_pairs_hook=_unique_object
    )
    _exact(header, ("protocol", "count"), "type project header")
    if (
        header["protocol"] != "python-type-project/v3"
        or type(header["count"]) is not int
        or not 0 <= header["count"] <= MAX_MODULES
    ):
        raise ValueError("type project header is invalid")
    modules = []
    size = 0
    for _ in range(header["count"]):
        record = source.readline(MAX_RECORD_BYTES + 1)
        size += len(record)
        if (
            len(record) > MAX_RECORD_BYTES
            or size > MAX_FACT_BYTES
            or not record.endswith(b"\n")
        ):
            raise ValueError(
                "type project exceeds its transport boundary or omits a source"
            )
        modules.append(json.loads(record, object_pairs_hook=_unique_object))
    if source.read(1):
        raise ValueError("type project has unexpected trailing input")
    result = {
        "protocol": header["protocol"],
        "covered": [
            {"path": item["path"], "sourceSha256": item["sourceSha256"]}
            for item in modules
        ],
        "reads": typed_dict_reads(modules),
        "imports": module_imports(modules),
    }
    encoded = json.dumps(
        result, ensure_ascii=False, sort_keys=True, separators=(",", ":")
    ).encode("utf-8")
    if len(encoded) > MAX_OUTPUT_BYTES:
        raise ValueError("resolved type facts exceed the response byte limit")
    output.write(encoded)


if __name__ == "__main__":
    try:
        resolve_stream(sys.stdin.buffer, sys.stdout.buffer)
    except (ValueError, TypeError) as error:
        sys.stderr.write(str(error) + "\n")
        raise SystemExit(2) from None
