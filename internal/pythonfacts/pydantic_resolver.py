from type_resolver import MAX_DEPTH, MAX_FACTS, _Resolver

ROOTS = {
    "pydantic.BaseModel",
    "pydantic.v1.BaseModel",
    "pydantic_settings.BaseSettings",
}
DECORATORS = {
    "pydantic.field_validator",
    "pydantic.model_validator",
    "pydantic.field_serializer",
    "pydantic.model_serializer",
    "pydantic.computed_field",
    "pydantic.validator",
    "pydantic.root_validator",
    "pydantic.v1.validator",
    "pydantic.v1.root_validator",
}
PRIVATE_FIELDS = {"pydantic.PrivateAttr", "pydantic.v1.PrivateAttr"}
FIELDS = {"pydantic.Field", "pydantic.v1.Field"} | PRIVATE_FIELDS
CLASS_VARS = {"typing.ClassVar", "typing_extensions.ClassVar"}
ANNOTATED = {"typing.Annotated", "typing_extensions.Annotated"}


class _PydanticResolver(_Resolver):
    def __init__(self, modules):
        super().__init__(modules)
        self.models = {}
        self.members = {}
        self.remaining = MAX_FACTS
        for module in self.files.values():
            for (scope, _), bindings in module["bindings"].items():
                if len(bindings) == 1 and not bindings[0]["conditional"]:
                    self.members.setdefault((module["path"], scope), []).append(
                        bindings[0]
                    )

    def reference(self, module, scope, name, seen=frozenset()):
        self.remaining -= 1
        if self.remaining < 0:
            raise ValueError("Pydantic resolution exceeds its work boundary")
        binding = self.binding(module, scope, name.partition(".")[0])
        if (
            binding is not None
            and binding["kind"] == "alias"
            and binding["value"]["kind"] == "name"
        ):
            target = self.binding(
                module, binding["scope"], binding["value"]["name"].partition(".")[0]
            )
            if self.late_binding(target, binding["scope"], binding["site"]):
                return None
        return super().reference(module, scope, name, seen)

    def late_binding(self, binding, scope, site):
        return (
            site is not None
            and binding is not None
            and binding["scope"] == scope
            and (binding["site"]["line"], binding["site"]["column"])
            >= (site["line"], site["column"])
        )

    def expression(self, module, scope, expression, site=None):
        if expression["kind"] not in {"name", "subscript", "call", "string"}:
            return None
        binding = self.binding(module, scope, expression["name"].partition(".")[0])
        if self.late_binding(binding, scope, site):
            return None
        return self.reference(module, scope, expression["name"])

    def model(self, reference, seen=frozenset()):
        if reference is None or isinstance(reference, str):
            return isinstance(reference, str) and reference in ROOTS
        path, binding = reference
        identity = path, binding["scope"], binding["name"]
        if identity in seen or len(seen) >= MAX_DEPTH:
            raise ValueError(
                "Pydantic inheritance is cyclic or exceeds its depth limit"
            )
        if identity not in self.models:
            self.models[identity] = self.model_binding(
                self.files[path], binding, seen | {identity}
            )
        return self.models[identity]

    def model_binding(self, module, binding, seen):
        if self.binding(module, binding["scope"], binding["name"]) is not binding:
            return False
        if binding["kind"] == "alias" and binding["value"]["kind"] == "subscript":
            return self.model(
                self.expression(
                    module, binding["scope"], binding["value"], binding["site"]
                ),
                seen,
            )
        if binding["kind"] != "class":
            return False
        return any(
            base["kind"] in {"name", "subscript"}
            and self.model(
                self.expression(module, binding["scope"], base, binding["site"]), seen
            )
            for base in binding["bases"]
        )

    def class_var(self, module, scope, annotation, seen=frozenset()):
        reference = self.expression(module, scope, annotation)
        if isinstance(reference, str):
            if reference in CLASS_VARS:
                return True
            return (
                reference in ANNOTATED
                and bool(annotation["args"])
                and self.class_var(module, scope, annotation["args"][0], seen)
            )
        if reference is None:
            return False
        path, binding = reference
        identity = path, binding["scope"], binding["name"]
        if identity in seen or len(seen) >= MAX_DEPTH:
            raise ValueError(
                "Pydantic field annotation is cyclic or exceeds its depth limit"
            )
        return binding["kind"] == "alias" and self.class_var(
            self.files[path], binding["scope"], binding["value"], seen | {identity}
        )

    def consumed(self, module, binding):
        kind, name, scope = binding["kind"], binding["name"], binding["scope"]
        if kind == "function":
            return any(
                isinstance(reference, str) and reference in DECORATORS
                for reference in (
                    self.expression(module, scope, decorator)
                    for decorator in binding["decorators"]
                )
            )
        if kind not in {"annotated", "alias"}:
            return False
        if kind == "annotated" and self.class_var(module, scope, binding["annotation"]):
            return False
        if name == "model_config":
            return True
        value = binding["value"]
        reference = (
            self.expression(module, scope, value) if value["kind"] == "call" else None
        )
        if kind == "annotated" and not name.startswith("_"):
            return True
        contracts = PRIVATE_FIELDS if name.startswith("_") else FIELDS
        return isinstance(reference, str) and reference in contracts

    def run(self):
        result = []
        for module in self.files.values():
            for binding in module["facts"]["bindings"]:
                if binding["kind"] != "class" or not self.model(
                    (module["path"], binding)
                ):
                    continue
                site = binding["site"]
                scope = f"{binding['scope']}/class:{site['line']}:{site['column']}"
                for member in self.members.get((module["path"], scope), ()):
                    if self.consumed(module, member):
                        result.append(self.definition_span(module, member))
        return sorted(
            result,
            key=lambda value: (
                value["path"],
                value["line"],
                value["end"],
                value["name"],
            ),
        )

    def definition_span(self, module, binding):
        line = binding["definitionLine"]
        return {
            "path": module["path"],
            "line": line,
            "end": binding["endSite"]["line"]
            if binding["kind"] == "function"
            else line,
            "name": binding["name"],
        }


def pydantic_members(modules):
    return _PydanticResolver(modules).run()
