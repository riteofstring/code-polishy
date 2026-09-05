import ast
import copy
import keyword
import unicodedata

PREDICATE = ast.parse(
    "_type(_value) is _str "
    "and _normalize('NFKC', _value) == _value "
    "and _value.count(':') == 1 "
    "and _value.startswith((_namespace_colon, _namespace_dot)) "
    "and _all(_part.isidentifier() and not _keyword(_part) "
    "for _part in _value.replace(':', '.').split('.'))",
    mode="eval",
).body


def _reference(node):
    if isinstance(node, ast.Name):
        return node.id
    if isinstance(node, ast.Attribute):
        prefix = _reference(node.value)
        return prefix + "." + node.attr if prefix else ""
    return ""


class _Substitute(ast.NodeTransformer):
    def __init__(self, replacements):
        self.replacements = replacements

    def visit_Name(self, node):
        if node.id in self.replacements:
            result = copy.deepcopy(self.replacements[node.id])
            result.ctx = node.ctx
            return result
        return node


def object_predicate(node):
    if not _predicate_function(node):
        return None
    try:
        return _predicate_expression(node)
    except (AttributeError, IndexError, TypeError, ValueError):
        return None


def _predicate_function(node):
    return (
        isinstance(node, ast.FunctionDef)
        and len(node.args.posonlyargs + node.args.args) == 1
        and not node.args.kwonlyargs
        and not node.args.vararg
        and not node.args.kwarg
        and not node.args.defaults
        and not node.decorator_list
        and not node.type_params
        and len(node.body) == 1
        and isinstance(node.body[0], ast.Return)
    )


def _predicate_expression(node):
    value = node.body[0].value
    if not isinstance(value, ast.BoolOp) or not isinstance(value.op, ast.And):
        return None
    if len(value.values) != 5:
        return None
    check_type, normalize, _, namespace, names = value.values
    parameter = (node.args.posonlyargs + node.args.args)[0].arg
    generator = names.args[0]
    part = generator.generators[0].target.id
    replacements = {
        "_type": check_type.left.func,
        "_str": check_type.comparators[0],
        "_normalize": normalize.left.func,
        "_all": names.func,
        "_keyword": generator.elt.values[1].operand.func,
        "_value": ast.Name(id=parameter, ctx=ast.Load()),
        "_part": ast.Name(id=part, ctx=ast.Load()),
    }
    references = {
        key[1:]: _reference(replacements[key])
        for key in ("_type", "_str", "_normalize", "_all", "_keyword")
    }
    if not all(references.values()) or any(
        reference.split(".")[0] in {parameter, part}
        for reference in references.values()
    ):
        return None
    prefixes = namespace.args[0].elts
    first, second = prefixes[0].value, prefixes[1].value
    if not isinstance(first, str) or not first.endswith(":"):
        return None
    if second != first[:-1] + ".":
        return None
    if unicodedata.normalize("NFKC", first) != first or not all(
        part.isidentifier() and not keyword.iskeyword(part)
        for part in first[:-1].split(".")
    ):
        return None
    replacements.update(
        _namespace_colon=ast.Constant(value=first),
        _namespace_dot=ast.Constant(value=second),
    )
    expected = _Substitute(replacements).visit(copy.deepcopy(PREDICATE))
    if ast.dump(value) != ast.dump(expected):
        return None
    return {"parameter": parameter, "namespace": first[:-1], "references": references}
