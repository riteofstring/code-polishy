package quality

const pythonVultureTypeFacts = `
def _vulture_read_source(path, filename):
    with open(filename, encoding="utf-8", newline="") as source_file:
        source = source_file.read(2 * 1024 * 1024 + 1)
    return source, parse_source(path, source)

def _vulture_type_modules(files):
    import hashlib
    modules = []
    size = 0
    for source in files:
        module = {
            "path": source["path"], "module": source["module"], "package": source["package"],
            "sourceSha256": hashlib.sha256(source["source"].encode("utf-8")).hexdigest(),
            "facts": type_facts(source["tree"]),
        }
        encoded_size = len(json.dumps(module, ensure_ascii=True, separators=(",", ":")).encode("utf-8"))
        size += encoded_size
        if encoded_size > 16 * 1024 * 1024 or size > 256 * 1024 * 1024:
            raise ValueError("Python type facts exceed their compact byte boundary")
        modules.append(module)
    return modules
`
