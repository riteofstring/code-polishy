from type_resolver import MAX_DEPTH, MAX_FACTS


class LoaderBindings:
    def __init__(self, facts, builtins=()):
        self.facts = facts
        self.builtins = frozenset(builtins)
        self.remaining = MAX_FACTS

    def reference(self, module, scope, name, site=None, flow=()):
        candidates = self.candidates(module, scope, name, site, flow)
        return next(iter(candidates)) if len(candidates) == 1 else ""

    def candidates(self, module, scope, name, site=None, flow=(), seen=frozenset()):
        self.remaining -= 1
        if self.remaining < 0 or len(seen) >= MAX_DEPTH:
            raise ValueError("Python loader binding exceeds its work boundary")
        identity = module["path"], scope, name
        if identity in seen:
            return {""}
        seen = seen | {identity}
        root, _, suffix = name.partition(".")
        values, wildcards, unbound = self.bindings(module, scope, root, site, flow)
        result = set()
        for binding in values:
            result.update(self.binding_targets(module, binding, suffix, seen))
        for binding in wildcards:
            imported = {**binding, "reference": binding["reference"][:-1] + root}
            result.update(self.binding_targets(module, imported, suffix, seen))
        if unbound and root in self.builtins:
            result.add("builtins." + name)
        if wildcards or any(value["conditional"] for value in values):
            result.add("")
        return result or {""}

    def bindings(self, module, scope, name, site, flow):
        wildcards = []
        while scope:
            wildcards.extend(module["bindings"].get((scope, "*"), []))
            values = module["bindings"].get((scope, name))
            if values is not None:
                return self.current_bindings(values, site, flow), wildcards, False
            parent = module["scopes"][scope]
            if parent["kind"] != "class":
                site = None
                flow = ()
            scope = parent["parent"]
        return [], wildcards, not wildcards

    def current_bindings(self, values, site, flow):
        if site is None:
            return values
        before = []
        for value in values:
            position = value["activationSite"]
            if (position["line"], position["column"]) >= (site["line"], site["column"]):
                continue
            if value["flow"] and list(flow[: len(value["flow"])]) == value["flow"]:
                value = {**value, "conditional": False}
            if not value["conditional"]:
                before = []
            before.append(value)
        return before

    def binding_targets(self, module, binding, suffix, seen):
        if (
            binding["kind"] in {"alias", "annotated"}
            and binding["value"]["kind"] == "name"
        ):
            name = binding["value"]["name"] + ("." + suffix if suffix else "")
            return self.candidates(
                module, binding["scope"], name, binding["site"], binding["flow"], seen
            )
        if binding["kind"] != "import":
            return {""}
        target = self.facts.import_reference(module, binding["reference"])
        return self.import_targets(target + ("." + suffix if suffix else ""), seen)

    def import_targets(self, target, seen):
        parts = target.split(".")
        for index in range(len(parts), 0, -1):
            candidates = self.facts.modules.get(".".join(parts[:index]))
            if candidates is None:
                continue
            if index == len(parts):
                return {""}
            result = set()
            for module in candidates:
                result.update(
                    self.candidates(
                        module, "module", ".".join(parts[index:]), seen=seen
                    )
                )
            if len(candidates) != 1:
                result.add("")
            return result
        return {""} if parts[0] in self.facts.module_roots else {target}
