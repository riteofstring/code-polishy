from unittest.mock import patch

from type_resolver import MAX_DEPTH

_MODELS = {
    "io": "class RawIOBase: pass",
    "socketserver": "class ThreadingMixIn: pass",
    "http.server": (
        "from socketserver import ThreadingMixIn\n"
        "class BaseHTTPRequestHandler: pass\nclass HTTPServer: pass\n"
        "class ThreadingHTTPServer(ThreadingMixIn, HTTPServer): pass"
    ),
}


class _AstroidContracts:
    def __init__(self, resolver, sources, contracts):
        from astroid import AstroidError, AstroidImportError, nodes
        from astroid.builder import AstroidBuilder
        from astroid.manager import AstroidManager

        self.manager_type = AstroidManager
        self.import_error = AstroidImportError
        self.errors = (AstroidError, RecursionError)
        self.class_type = nodes.ClassDef
        self.resolver = resolver
        self.sources = {source["path"]: source for source in sources}
        self.manager = AstroidManager()
        self.builder = AstroidBuilder(self.manager, apply_transforms=False)
        self.cache = {}
        self.paths = {}
        self.override = patch.object(
            self.manager_type, "ast_from_module_name", self.load
        )
        self.declared_roots = {
            contract["target"] for contract in contracts if contract["kind"] == "type"
        }
        self.models = dict(_MODELS)
        for contract in contracts:
            if contract["kind"] == "type":
                module, _, name = contract["target"].rpartition(".")
                model = self.models.get(module, "")
                if f"class {name}:" not in model and f"class {name}(" not in model:
                    self.models[module] = model + f"\nclass {name}: pass\n"

    def __enter__(self):
        self.override.__enter__()
        return self

    def __exit__(self, kind, value, traceback):
        return self.override.__exit__(kind, value, traceback)

    def load(self, name, context_file=None, use_cache=True):
        if context_file is not None and context_file not in self.sources:
            raise self.import_error("Framework inference requested external source")
        if use_cache and name in self.cache:
            return self.cache[name]
        candidates = self.resolver.modules.get(name, [])
        if len(candidates) == 1:
            source = self.sources[candidates[0]["path"]]
            module = self.builder.string_build(source["source"], name, source["path"])
            module.package = (
                source["path"].endswith("/__init__.py")
                or source["path"] == "__init__.py"
            )
            self.paths[name] = source["path"]
        elif candidates or name.split(".")[0] in self.resolver.module_roots:
            raise self.import_error(f"Unresolved contained framework module: {name}")
        elif name in self.models:
            module = self.builder.string_build(self.models[name], name)
        elif any(model.startswith(name + ".") for model in self.models):
            module = self.builder.string_build("", name)
            module.package = True
        else:
            raise self.import_error(f"Unadmitted framework module: {name}")
        self.cache[name] = module
        return module

    def derives(self, reference, roots):
        if isinstance(reference, str):
            return reference in roots
        if reference is None:
            return False
        path, binding = reference
        if binding["kind"] != "class":
            return False
        try:
            module = self.load(self.resolver.files[path]["module"])
            classes = [
                node
                for node in module.nodes_of_class(self.class_type)
                if node.name == binding["name"]
                and node.lineno == binding["site"]["line"]
            ]
            if len(classes) != 1:
                return False
            return self.has_base(classes[0], roots, set())
        except self.errors:
            return False

    def has_base(self, node, roots, seen):
        from astroid import nodes

        if node in seen or len(seen) >= MAX_DEPTH:
            return False
        seen = seen | {node}
        if node.qname() in roots & self.declared_roots:
            return True
        for base in node.bases:
            names = list(base.nodes_of_class(nodes.Name))
            if any(not self.unique_binding(name) for name in names):
                continue
            inferred = list(base.infer())
            if len(inferred) != 1 or not isinstance(inferred[0], nodes.ClassDef):
                continue
            parent = inferred[0]
            if parent.root().name not in self.paths and parent.qname() in roots:
                return True
            if self.has_base(parent, roots, seen):
                return True
        return False

    def unique_binding(self, node):
        definitions = node.lookup(node.name)[1]
        return len(definitions) == 1 and (
            definitions[0].root() is not node.root()
            or definitions[0].lineno <= node.lineno
        )

    def entry_point(self, target, members):
        module_name, _, symbol = target.partition(":")
        module = self.load(module_name)
        if module_name not in self.paths:
            raise ValueError(f"entry-point module is not project source: {module_name}")
        current = module
        kept = set()
        for part in symbol.split("."):
            current = self.member(current, part, kept)
        for name in members:
            self.member(current, name, kept)
        return kept

    def member(self, owner, name, kept):
        from astroid import bases, nodes

        definitions = owner.getattr(name)
        if len(definitions) != 1:
            raise ValueError(f"entry-point member is stale or ambiguous: {name}")
        definition = definitions[0]
        self.unconditional(definition)
        path = self.paths.get(definition.root().name)
        if path is None:
            raise ValueError(f"entry-point member is not project source: {name}")
        start = definition.fromlineno
        end = (
            definition.tolineno
            if isinstance(definition, (nodes.FunctionDef, nodes.ClassDef))
            else start
        )
        kept.add((path, start, end, name))
        inferred = []
        for value in definition.infer():
            if value not in inferred:
                inferred.append(value)
            if len(inferred) > 1:
                break
        if len(inferred) != 1 or not isinstance(
            inferred[0],
            (nodes.Module, nodes.ClassDef, nodes.FunctionDef, bases.Instance),
        ):
            raise ValueError(f"entry-point member cannot resolve uniquely: {name}")
        return inferred[0]

    def unconditional(self, definition):
        from astroid import nodes

        current = definition.parent
        while current is not None:
            if isinstance(
                current, (nodes.If, nodes.For, nodes.While, nodes.Try, nodes.With)
            ):
                raise TypeError("entry-point definition is conditional")
            if isinstance(current, (nodes.FunctionDef, nodes.ClassDef, nodes.Module)):
                return
            current = current.parent
