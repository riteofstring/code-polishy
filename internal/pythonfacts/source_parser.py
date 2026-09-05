import ast
import io
import tokenize

MAX_TOKENS = 500000
MAX_AST_NODES = 200000
MAX_AST_DEPTH = 256
MAX_SOURCE_BYTES = 2 * 1024 * 1024


def parse_source(path, source):
    if len(source.encode("utf-8")) > MAX_SOURCE_BYTES:
        raise ValueError("Python source exceeds the per-source byte limit")
    tokens = list(tokenize.generate_tokens(io.StringIO(source).readline))
    if len(tokens) > MAX_TOKENS:
        raise ValueError("Python source exceeds the token limit")
    tree = ast.parse(source, filename=path, type_comments=True, feature_version=(3, 12))
    count = 0
    stack = [(tree, 1)]
    while stack:
        node, depth = stack.pop()
        count += 1
        if count > MAX_AST_NODES:
            raise ValueError("Python source exceeds the AST node limit")
        if depth > MAX_AST_DEPTH:
            raise ValueError("Python source exceeds the AST depth limit")
        stack.extend((child, depth + 1) for child in ast.iter_child_nodes(node))
    return tree
