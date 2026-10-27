import { doc } from "prettier";
import { parsers, printers } from "prettier/plugins/markdown";

const ADMONITION =
  /(?:^|\n)[ \t]*(?:>[ \t]*|(?:[-+*]|\d+[.)])[ \t]+)*(?:!!!|\?\?\?\+?)(?=[ \t]|$)/;
const PRESERVED = "preservedMarkdown";

function containsAdmonition(node, source) {
  if (node.type === "paragraph") {
    return ADMONITION.test(
      source.slice(node.position.start.offset, node.position.end.offset),
    );
  }
  if (node.type === "code") {
    return ADMONITION.test(node.value);
  }
  return (
    node.children?.some((child) => containsAdmonition(child, source)) ?? false
  );
}

function lineStart(source, node) {
  return source.lastIndexOf("\n", node.position.start.offset - 1) + 1;
}

function indentedBlock(source, node) {
  return /^(?: {4}|\t)/.test(source.slice(lineStart(source, node)));
}

function takePreviousLists(children, first) {
  while (children.at(-1)?.type === "list") {
    first = children.pop();
  }
  return first;
}

function preserveAdmonitions(source, options) {
  const root = parsers.markdown.parse(source, options);
  const children = [];
  for (let index = 0; index < root.children.length; index++) {
    let first = root.children[index];
    if (!containsAdmonition(first, source)) {
      children.push(first);
      continue;
    }
    let last = first;
    if (first.type === "list") {
      first = takePreviousLists(children, first);
      while (root.children[index + 1]?.type === "list") {
        last = root.children[++index];
      }
    }
    while (
      index + 1 < root.children.length &&
      indentedBlock(source, root.children[index + 1])
    ) {
      last = root.children[++index];
    }
    children.push({
      type: PRESERVED,
      value: source.slice(lineStart(source, first), last.position.end.offset),
      position: { start: first.position.start, end: last.position.end },
    });
  }
  root.children = children;
  return root;
}

export const markdownPlugin = {
  parsers: {
    markdown: { ...parsers.markdown, parse: preserveAdmonitions },
  },
  printers: {
    mdast: {
      ...printers.mdast,
      getVisitorKeys(node) {
        return node.type === PRESERVED
          ? []
          : printers.mdast.getVisitorKeys(node);
      },
      print(path, options, print) {
        if (path.node.type === PRESERVED) {
          return doc.builders.join(
            doc.builders.literalline,
            path.node.value.split("\n"),
          );
        }
        const printed = printers.mdast.print(path, options, print);
        const last =
          path.node.type === "root" ? path.node.children.at(-1) : null;
        if (last?.type !== PRESERVED) {
          return printed;
        }
        const newlines = Math.max(1, last.value.match(/\n*$/)[0].length);
        return [
          doc.utils.stripTrailingHardline(printed),
          Array(newlines).fill(doc.builders.literalline),
        ];
      },
    },
  },
};
