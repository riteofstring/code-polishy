import keyword


def module_name(value):
    return (
        isinstance(value, str)
        and bool(value)
        and all(
            part.isidentifier() and not keyword.iskeyword(part)
            for part in value.split(".")
        )
    )


class ModuleEvidence:
    def __init__(self, module, bindings):
        self.module = module
        self.bindings = bindings
        self.calls = {self.location(call): call for call in module["facts"]["calls"]}

    def location(self, expression):
        return tuple(
            expression[site][coordinate]
            for site in ("site", "endSite")
            for coordinate in ("line", "column")
        )

    def call(self, expression, expected):
        call = self.calls.get(self.location(expression))
        if (
            call is None
            or call["endSite"] != expression["endSite"]
            or call["callee"]["kind"] != "name"
        ):
            return None
        callee = self.bindings.reference(
            self.module,
            call["scope"],
            call["callee"]["value"],
            call["site"],
            call["flow"],
        )
        return call if callee == expected else None

    def literal_targets(self, expression):
        kind = expression["kind"]
        if kind == "string" and module_name(expression["value"]):
            return {expression["value"]}
        children = expression["children"]
        if kind == "choice":
            left, right = [self.literal_targets(value) for value in children]
            return left | right if left is not None and right is not None else None
        if kind == "subscript" and children[0]["kind"] == "collection":
            values = children[0]["children"]
            if values and all(
                value["kind"] == "string" and module_name(value["value"])
                for value in values
            ):
                return {value["value"] for value in values}
        return None

    def path_read(self, expression):
        call = self.call(expression, "json.loads")
        if call is None or len(call["arguments"]) != 1 or call["keywords"]:
            return ""
        read = call["arguments"][0]
        read = self.calls.get(self.location(read))
        if (
            read is None
            or read["arguments"]
            or read["callee"]["kind"] != "attribute"
            or read["callee"]["value"] != "read_text"
        ):
            return ""
        path = self.call(read["callee"]["children"][0], "pathlib.Path")
        if path is None or len(path["arguments"]) != 1 or path["keywords"]:
            return ""
        value = path["arguments"][0]
        if value["kind"] != "string" or not value["value"]:
            return ""
        for item in read["keywords"]:
            encoding = item["value"]
            if (
                item["name"] != "encoding"
                or encoding["kind"] != "string"
                or encoding["value"].lower().replace("_", "-") != "utf-8"
            ):
                return ""
        return value["value"]

    def configuration(self, expression):
        selectors = []
        while expression["kind"] == "subscript":
            expression, selector = expression["children"]
            selectors.append(selector)
        path = self.path_read(expression)
        if not path:
            return None
        selectors.reverse()
        segments = []
        for index, selector in enumerate(selectors):
            if selector["kind"] in {"string", "integer"}:
                segments.append(selector["value"])
            elif index != len(selectors) - 1:
                return None
        if not segments:
            return None
        pointer = "/" + "/".join(
            value.replace("~", "~0").replace("/", "~1") for value in segments
        )
        return path, pointer

    def entry_point(self, expression):
        if expression["kind"] != "attribute" or expression["value"] != "module":
            return ""
        selection = expression["children"][0]
        if selection["kind"] == "subscript":
            expression = selection["children"][0]
        else:
            selection = self.call(selection, "builtins.next")
            if (
                selection is None
                or len(selection["arguments"]) != 1
                or selection["keywords"]
            ):
                return ""
            expression = selection["arguments"][0]
        call = self.call(expression, "importlib.metadata.entry_points")
        if call is None or call["arguments"] or len(call["keywords"]) != 1:
            return ""
        group = call["keywords"][0]
        if group["name"] != "group" or group["value"]["kind"] != "string":
            return ""
        return group["value"]["value"]

    def evidence(self, expression):
        targets = self.literal_targets(expression)
        if targets is not None:
            return targets, set(), ""
        configuration = self.configuration(expression)
        if configuration is not None:
            return set(), {configuration}, ""
        group = self.entry_point(expression)
        if group:
            return set(), set(), group
        if expression["kind"] == "choice":
            left, right = [self.evidence(value) for value in expression["children"]]
            if (
                left is not None
                and right is not None
                and (not left[2] or not right[2] or left[2] == right[2])
            ):
                return left[0] | right[0], left[1] | right[1], left[2] or right[2]
        return None
