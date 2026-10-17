import importlib.metadata
import json
import platform
import sys

import tree_sitter_rust
from tree_sitter import Language, Parser

PROTOCOL = "rust-source/v2"
MAXIMUM_REQUEST = 8 * 1024 * 1024
MAXIMUM_NODES = 500_000
COMMENTS = {"line_comment", "block_comment"}
CALLABLES = {"function_item", "closure_expression"}
BRANCHES = {
    "if_expression",
    "for_expression",
    "while_expression",
    "loop_expression",
    "try_expression",
    "or_pattern",
}
BUILTIN_ATTRIBUTES = {
    "cfg",
    "cfg_attr",
    "allow",
    "expect",
    "deny",
    "forbid",
    "doc",
    "path",
    "test",
    "ignore",
    "should_panic",
    "inline",
    "cold",
    "must_use",
    "repr",
    "no_std",
    "no_main",
    "macro_export",
    "unsafe",
    "deprecated",
    "non_exhaustive",
    "track_caller",
    "automatically_derived",
    "no_mangle",
    "export_name",
    "link",
    "link_name",
    "link_section",
    "link_ordinal",
    "used",
    "target_feature",
    "instruction_set",
    "panic_handler",
    "global_allocator",
    "recursion_limit",
    "type_length_limit",
    "windows_subsystem",
    "crate_type",
    "crate_name",
    "proc_macro",
    "proc_macro_derive",
    "proc_macro_attribute",
}
BUILTIN_DERIVES = {
    "Clone",
    "Copy",
    "Debug",
    "Default",
    "Eq",
    "PartialEq",
    "Ord",
    "PartialOrd",
    "Hash",
}
EXPRESSION_MACROS = {
    "assert",
    "assert_eq",
    "assert_ne",
    "debug_assert",
    "debug_assert_eq",
    "debug_assert_ne",
    "format_args",
    "panic",
    "todo",
    "unimplemented",
    "unreachable",
    "write",
    "writeln",
    "concat",
    "env",
    "option_env",
    "compile_error",
    "include_bytes",
    "include_str",
    "file",
    "line",
    "column",
    "module_path",
}
STANDARD_MACROS = {"dbg", "print", "println", "eprint", "eprintln", "format"}
LEXICAL_SCOPES = {
    "function_item",
    "function_signature_item",
    "closure_expression",
    "block",
    "impl_item",
    "trait_item",
    "struct_item",
    "enum_item",
    "type_item",
    "union_item",
}


def location(node, source):
    start_row, start_column = node.start_point
    end_row, end_column = node.end_point
    return {
        "startByte": node.start_byte,
        "endByte": node.end_byte,
        "line": start_row + 1,
        "column": len(source[node.start_byte - start_column : node.start_byte].decode())
        + 1,
        "endLine": end_row + 1,
        "endColumn": len(source[node.end_byte - end_column : node.end_byte].decode())
        + 1,
    }


def path_name(node):
    parts = []
    pending = [node]
    while pending:
        current = pending.pop()
        if current.type in {"identifier", "crate", "self", "super"}:
            parts.append(current.text.decode().removeprefix("r#"))
        elif current.type not in COMMENTS:
            pending.extend(reversed(current.named_children))
    return "::".join(parts)


def token_groups(node):
    groups = [[]]
    for token in node.children[1:-1]:
        if token.type == ",":
            groups.append([])
        elif token.type not in COMMENTS:
            groups[-1].append(token)
    return [group for group in groups if group]


def attribute_tokens(tokens, source, depth=0):
    if depth > 64:
        raise ValueError("Rust attribute nesting exceeds 64 levels")
    parts = []
    position = 0
    while position < len(tokens) and tokens[position].type in {
        "identifier",
        "scoped_identifier",
        "::",
    }:
        if tokens[position].type != "::":
            parts.append(path_name(tokens[position]))
        position += 1
    name = "::".join(parts)
    arguments = next((token for token in tokens if token.type == "token_tree"), None)
    groups = token_groups(arguments) if arguments is not None else []
    children = []
    if name == "cfg_attr":
        children = [attribute_tokens(group, source, depth + 1) for group in groups[1:]]
    return {
        **location(tokens[0], source),
        "name": name,
        "hasValue": any(token.type == "=" for token in tokens),
        "testConfiguration": name == "cfg"
        and len(groups) == 1
        and len(groups[0]) == 1
        and groups[0][0].type == "identifier"
        and groups[0][0].text == b"test",
        "children": children,
    }


def attribute(node, source):
    value = node.named_children[0]
    tokens = [child for child in value.children if child.type not in COMMENTS]
    if not tokens:
        raise ValueError("Rust attribute omitted its parsed path")
    return attribute_tokens(tokens, source)


def item_attributes(node, source):
    attributes = []
    sibling = node.prev_named_sibling
    while sibling is not None and sibling.type in COMMENTS | {"attribute_item"}:
        if sibling.type == "attribute_item":
            attributes.append(attribute(sibling, source))
        sibling = sibling.prev_named_sibling
    return attributes


def test_configuration(node, source):
    attributes = item_attributes(node, source)
    body = node.child_by_field_name("body")
    scope = body if body is not None else node
    attributes.extend(
        attribute(child, source)
        for child in scope.named_children
        if child.type == "inner_attribute_item"
    )
    return any(value["testConfiguration"] for value in attributes)


def function_fact(node, source, test_context):
    name = node.child_by_field_name("name")
    parameters = node.child_by_field_name("parameters")
    attributes = item_attributes(node, source)
    entry = (
        test_entry_attributes(attributes) if node.type == "function_item" else "none"
    )
    return {
        **location(node, source),
        "name": name.text.decode() if name is not None else "<closure>",
        "context": "test" if test_context or entry == "builtin" else "production",
        "testEntry": entry,
        "body": function_body(node),
        "parameters": sum(
            child.type not in COMMENTS | {"attribute_item"}
            for child in parameters.named_children
        )
        if parameters is not None
        else 0,
        "complexity": 1,
        "depth": 0,
    }


def test_entry_attributes(attributes):
    if any(value["name"] == "test" for value in attributes):
        return "builtin"
    if any(
        value["name"] == "cfg_attr"
        and test_entry_attributes(value["children"]) != "none"
        for value in attributes
    ):
        return "conditional-builtin"
    return "none"


def function_body(node):
    body = node.child_by_field_name("body")
    if body is None:
        raise ValueError("Rust function omitted its body")
    if body.type != "block":
        return "nonempty"
    non_behavior = COMMENTS | {
        "attribute_item",
        "inner_attribute_item",
        "empty_statement",
    }
    return (
        "nonempty"
        if any(child.type not in non_behavior for child in body.named_children)
        else "empty"
    )


def decisions(node):
    if node.type in BRANCHES:
        return 1
    if node.type == "binary_expression":
        operator = node.child_by_field_name("operator")
        return int(operator is not None and operator.type in {"&&", "||"})
    if node.type == "match_expression":
        body = node.child_by_field_name("body")
        return max(
            0, sum(child.type == "match_arm" for child in body.named_children) - 1
        )
    if node.type in {"match_pattern", "let_declaration"}:
        field = "condition" if node.type == "match_pattern" else "alternative"
        return int(node.child_by_field_name(field) is not None)
    return 0


def collect_node(node, source, result):
    if node.type in COMMENTS:
        result["comments"].append(location(node, source))
    elif node.type in {"attribute_item", "inner_attribute_item"}:
        result["attributes"].append(attribute(node, source))
    elif node.type in {"macro_invocation", "macro_definition"}:
        name = node.child_by_field_name(
            "macro" if node.type == "macro_invocation" else "name"
        )
        result["macros"].append(
            {
                **location(node, source),
                "name": path_name(name) if name is not None else "",
                "definition": node.type == "macro_definition",
            }
        )


def syntax_path(node):
    if node is None:
        return []
    if node.type in {
        "identifier",
        "type_identifier",
        "crate",
        "self",
        "super",
        "primitive_type",
    }:
        return [node.text.decode().removeprefix("r#")]
    if node.type in {"scoped_identifier", "scoped_type_identifier"}:
        return syntax_path(node.child_by_field_name("path")) + syntax_path(
            node.child_by_field_name("name")
        )
    if node.type in {"generic_type", "generic_function"}:
        field = "type" if node.type == "generic_type" else "function"
        return syntax_path(node.child_by_field_name(field))
    return []


def use_paths(node, prefix=None):
    prefix = [] if prefix is None else prefix
    if node.type == "use_list":
        return [
            value for child in node.named_children for value in use_paths(child, prefix)
        ]
    if node.type == "scoped_use_list":
        base = prefix + syntax_path(node.child_by_field_name("path"))
        return use_paths(node.child_by_field_name("list"), base)
    if node.type == "use_as_clause":
        path = prefix + syntax_path(node.child_by_field_name("path"))
        alias = node.child_by_field_name("alias").text.decode().removeprefix("r#")
    elif node.type == "use_wildcard":
        path = prefix + syntax_path(next(iter(node.named_children), None))
        alias = "*"
    else:
        path = prefix + syntax_path(node)
        alias = path[-1] if path else ""
    if path and path[-1] == "self":
        path = path[:-1]
        alias = path[-1] if alias == "self" and path else alias
    return [(node, path, alias)]


def graph_fact(node, source, scope, kind, **values):
    return {
        **location(node, source),
        "scope": scope,
        "kind": kind,
        "name": "",
        "path": [],
        "alias": "",
        "inline": False,
        "file": "",
        "public": False,
        **values,
    }


def module_file(node, source):
    attributes = item_attributes(node, source)
    if any(value["name"] == "cfg_attr" for value in attributes):
        return "", "conditional module attributes need resolved path evidence"
    sibling = node.prev_named_sibling
    while sibling is not None and sibling.type in COMMENTS | {"attribute_item"}:
        if sibling.type == "attribute_item":
            value = sibling.named_children[0]
            if path_name(value.named_children[0]) == "path":
                literal = value.child_by_field_name("value")
                try:
                    return json.loads(literal.text.decode()), ""
                except (ValueError, AttributeError):
                    return "", "module path requires a supported literal string"
        sibling = sibling.prev_named_sibling
    return "", ""


def collect_graph_node(node, source, result, scope):
    facts = result["graph"]
    if node.type in {"attribute_item", "inner_attribute_item"}:
        facts.extend(attribute_graph_facts(node, source, scope))
    elif node.type == "mod_item":
        name = node.child_by_field_name("name").text.decode().removeprefix("r#")
        file, problem = module_file(node, source)
        kind = "unsupported" if problem else "module"
        facts.append(
            graph_fact(
                node,
                source,
                scope,
                kind,
                name=problem or name,
                file=file,
                inline=node.child_by_field_name("body") is not None,
            )
        )
    elif node.type == "use_declaration":
        facts.extend(use_graph_facts(node, source, scope))
    elif node.type in {"scoped_identifier", "scoped_type_identifier"}:
        facts.extend(reference_graph_facts(node, source, scope))
    elif node.type in {
        "function_item",
        "struct_item",
        "enum_item",
        "type_item",
        "trait_item",
        "const_item",
        "static_item",
        "union_item",
        "type_parameter",
    }:
        name = node.child_by_field_name("name")
        if name is not None:
            facts.append(
                graph_fact(
                    node,
                    source,
                    scope,
                    "item",
                    name=name.text.decode().removeprefix("r#"),
                )
            )
    elif node.type == "extern_crate_declaration":
        name = node.child_by_field_name("name").text.decode().removeprefix("r#")
        alias = node.child_by_field_name("alias")
        facts.append(
            graph_fact(
                node,
                source,
                scope,
                "extern",
                path=[name],
                alias=alias.text.decode() if alias is not None else name,
            )
        )
    elif node.type in {"macro_invocation", "macro_definition"}:
        facts.extend(expansion_graph_facts(node, source, scope))


def expansion_graph_facts(node, source, scope):
    if node.type == "macro_invocation":
        return macro_graph_facts(node, source, scope)
    return [
        graph_fact(
            node,
            source,
            scope,
            "unsupported",
            name="macro expansion requires resolved import evidence",
        )
    ]


def use_graph_facts(node, source, scope):
    if node.child_by_field_name("argument").text.startswith(b"::"):
        return [
            graph_fact(
                node,
                source,
                scope,
                "unsupported",
                name="absolute use paths need external-prelude resolution",
            )
        ]
    public = any(child.type == "visibility_modifier" for child in node.named_children)
    return [
        graph_fact(
            position, source, scope, "use", path=path, alias=alias, public=public
        )
        for position, path, alias in use_paths(node.child_by_field_name("argument"))
    ]


def reference_graph_facts(node, source, scope):
    excluded = {
        "use_declaration",
        "attribute_item",
        "inner_attribute_item",
        "visibility_modifier",
        "macro_invocation",
    }
    if any(parent.type in excluded for parent in node_ancestors(node)):
        return []
    if node.text.startswith(b"::"):
        return [
            graph_fact(
                node,
                source,
                scope,
                "unsupported",
                name="absolute paths need external-prelude resolution",
            )
        ]
    path = syntax_path(node)
    return [graph_fact(node, source, scope, "reference", path=path)] if path else []


def attribute_graph_facts(node, source, scope):
    parsed = attribute(node, source)
    if parsed["name"] in {"no_std", "no_implicit_prelude"}:
        return [graph_fact(node, source, scope, "prelude", name=parsed["name"])]
    if parsed["name"] == "derive":
        arguments = node.named_children[0].child_by_field_name("arguments")
        groups = token_groups(arguments) if arguments is not None else []
        if groups and all(
            len(group) == 1 and group[0].text.decode() in BUILTIN_DERIVES
            for group in groups
        ):
            return [
                graph_fact(
                    group[0],
                    source,
                    scope,
                    "macro",
                    name="core",
                    path=[group[0].text.decode()],
                )
                for group in groups
            ]
    elif known_graph_attribute(parsed):
        return []
    return [
        graph_fact(
            node,
            source,
            scope,
            "unsupported",
            name="attribute expansion requires resolved import evidence",
        )
    ]


def known_graph_attribute(parsed):
    return parsed["name"] in BUILTIN_ATTRIBUTES - {"no_std"} and all(
        known_graph_attribute(child) for child in parsed["children"]
    )


def macro_graph_facts(node, source, scope, depth=0):
    macro = node.child_by_field_name("macro")
    path = syntax_path(macro)
    if not path or depth >= 64:
        raise ValueError("Rust macro syntax is missing or exceeds 64 levels")
    name = path[-1]
    tokens = next(child for child in node.named_children if child.type == "token_tree")
    facts = [
        graph_fact(
            node,
            source,
            scope,
            "macro",
            path=path,
            name="std"
            if name in STANDARD_MACROS
            else "alloc"
            if name == "vec"
            else "core",
        )
    ]
    try:
        if macro.text.startswith(b"::"):
            raise ValueError("Absolute macro paths need external-prelude resolution")
        for prefix, start, end, suffix in macro_fragments(name, tokens):
            facts.extend(
                parse_graph_fragment(prefix, start, end, suffix, source, scope, depth)
            )
    except ValueError:
        return [
            graph_fact(
                node,
                source,
                scope,
                "unsupported",
                name="macro arguments or expansion require resolved import evidence",
            )
        ]
    return facts


def macro_fragments(name, tokens):
    start, end = tokens.start_byte + 1, tokens.end_byte - 1
    if name in EXPRESSION_MACROS | STANDARD_MACROS:
        return [(b"fn cp(){cp(", start, end, b");}")]
    if name == "vec":
        return [(b"fn cp(){let _=[", start, end, b"];}")]
    if name in {"stringify", "cfg"}:
        return []
    if name != "matches":
        raise ValueError("Unsupported macro syntax")
    children = [child for child in tokens.children[1:-1] if child.type not in COMMENTS]
    comma = next((child for child in children if child.type == ","), None)
    if comma is None:
        raise ValueError("matches requires an expression and a pattern")
    if children[-1].type == ",":
        end = children[-1].start_byte
    return [
        (b"fn cp(){cp(", start, comma.start_byte, b");}"),
        (b"fn cp(){match (){", comma.end_byte, end, b" => ()}}"),
    ]


def parse_graph_fragment(prefix, start, end, suffix, source, scope, depth):
    fragment = prefix + source[start:end] + suffix
    tree = Parser(Language(tree_sitter_rust.language())).parse(fragment)
    if tree.root_node.has_error:
        raise ValueError("Macro arguments do not match supported Rust syntax")
    facts = {"graph": []}
    pending = [(tree.root_node, scope)]
    visited = 0
    while pending:
        current, current_scope = pending.pop()
        visited += 1
        if visited > MAXIMUM_NODES:
            raise ValueError("Rust macro arguments exceed 500000 syntax nodes")
        within = current.start_byte >= len(prefix) and current.end_byte <= len(
            fragment
        ) - len(suffix)
        if within:
            if current.type == "macro_invocation":
                facts["graph"].extend(
                    macro_graph_facts(current, fragment, current_scope, depth + 1)
                )
                continue
            current_scope = collect_graph_scope(current, fragment, facts, current_scope)
        pending.extend(
            (child, current_scope) for child in reversed(current.named_children)
        )
    for fact in facts["graph"]:
        fact["scope"] = fact["scope"][: len(scope)] + [
            shifted_scope(value, start - len(prefix))
            for value in fact["scope"][len(scope) :]
        ]
        if fact["kind"] == "scope":
            fact["name"] = shifted_scope(fact["name"], start - len(prefix))
        fact.update(
            offset_location(
                source,
                fact["startByte"] + start - len(prefix),
                fact["endByte"] + start - len(prefix),
            )
        )
    return facts["graph"]


def shifted_scope(name, offset):
    return "@" + str(int(name[1:]) + offset) if name.startswith("@") else name


def collect_graph_scope(node, source, result, scope):
    collect_graph_node(node, source, result, scope)
    if node.type in LEXICAL_SCOPES:
        name = "@" + str(node.start_byte)
        result["graph"].append(graph_fact(node, source, scope, "scope", name=name))
        return [*scope, name]
    if node.type == "mod_item" and node.child_by_field_name("body") is not None:
        return [
            *scope,
            node.child_by_field_name("name").text.decode().removeprefix("r#"),
        ]
    return scope


def offset_location(source, start, end):
    return {
        "startByte": start,
        "endByte": end,
        "line": source.count(b"\n", 0, start) + 1,
        "column": len(source[source.rfind(b"\n", 0, start) + 1 : start].decode()) + 1,
        "endLine": source.count(b"\n", 0, end) + 1,
        "endColumn": len(source[source.rfind(b"\n", 0, end) + 1 : end].decode()) + 1,
    }


def node_ancestors(node):
    parent = node.parent
    while parent is not None:
        yield parent
        parent = parent.parent


class SyntaxCollector:
    def __init__(self, source, result):
        self.source = source
        self.result = result

    def walk(self, root):
        pending = [(root, None, False, 0, [])]
        visited = 0
        while pending:
            visited += 1
            if visited > MAXIMUM_NODES:
                raise ValueError("Rust syntax exceeds 500000 nodes")
            pending.extend(self.visit(*pending.pop()))

    def visit(self, node, function, test_context, depth, scope):
        if node.type in {"source_file", "mod_item", "function_item"}:
            test_context = test_context or test_configuration(node, self.source)
        collect_node(node, self.source, self.result)
        scope = collect_graph_scope(node, self.source, self.result, scope)
        body = None
        if node.type in CALLABLES:
            body = node.child_by_field_name("body")
            function = function_fact(node, self.source, test_context)
            self.result["functions"].append(function)
            test_context = function["context"] == "test"
            depth = 0
        elif function is not None:
            function["complexity"] += decisions(node)
            depth += int(node.type == "block")
            function["depth"] = max(function["depth"], depth)
        children = []
        if node.type not in COMMENTS:
            for child in reversed(node.named_children):
                owner = function if body is None or child == body else None
                children.append((child, owner, test_context, depth, scope))
        return children


def source_result(entry, error=""):
    return {
        "path": entry["path"],
        "complete": False,
        "error": error,
        "comments": [],
        "attributes": [],
        "functions": [],
        "macros": [],
        "graph": [],
    }


def source_facts(parser, entry):
    source = entry["source"].encode("utf-8")
    tree = parser.parse(source)
    result = source_result(entry)
    if tree is None or tree.root_node.has_error:
        result["error"] = (
            "Rust syntax is incomplete or unsupported by the pinned grammar"
        )
        return result
    try:
        SyntaxCollector(source, result).walk(tree.root_node)
        result["complete"] = True
    except ValueError as error:
        result["error"] = str(error)
    return result


def read_request():
    data = sys.stdin.buffer.read(MAXIMUM_REQUEST + 1)
    if len(data) > MAXIMUM_REQUEST:
        raise ValueError("Rust syntax request exceeds 8388608 bytes")
    request = json.loads(data)
    if set(request) != {"protocol", "sources"} or request["protocol"] != PROTOCOL:
        raise ValueError("Unsupported Rust syntax request")
    if (
        not isinstance(request["sources"], list)
        or not 1 <= len(request["sources"]) <= 4096
    ):
        raise ValueError("Rust syntax request requires between 1 and 4096 sources")
    for entry in request["sources"]:
        if set(entry) != {"path", "source"} or not all(
            isinstance(value, str) for value in entry.values()
        ):
            raise ValueError("Rust syntax source requires a path and UTF-8 text")
    return request


def main():
    request = read_request()
    parser = Parser(Language(tree_sitter_rust.language()))
    metadata = {
        "protocol": PROTOCOL,
        "pythonVersion": platform.python_version(),
        "parserVersion": importlib.metadata.version("tree-sitter"),
        "grammarVersion": importlib.metadata.version("tree-sitter-rust"),
    }
    for entry in request["sources"]:
        result = source_facts(parser, entry)
        output = json.dumps({**metadata, "sources": [result]})
        if len(output.encode("utf-8")) + 1 > MAXIMUM_REQUEST:
            result = source_result(
                entry, "Rust syntax source record exceeds 8388608 bytes"
            )
            output = json.dumps({**metadata, "sources": [result]})
        sys.stdout.write(output + "\n")


if __name__ == "__main__":
    main()
