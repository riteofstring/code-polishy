import { Linter } from "eslint";
import eslintInternals from "eslint/use-at-your-own-risk";

const BOUNDARIES = new Set([
  "FunctionDeclaration",
  "FunctionExpression",
  "ArrowFunctionExpression",
  "StaticBlock",
  "Program",
]);

export function functionMetrics(analysis, path, parsed, offset = 0) {
  const facts = new Map();
  function factFor(node) {
    if (!facts.has(node))
      facts.set(node, {
        path,
        ...analysis.location(path, offset + node.range[0]),
        name: functionName(node),
        complexity: 1,
        depth: 0,
        parameters: 0,
      });
    return facts.get(node);
  }
  const rules = {};
  for (const [name, field] of [
    ["complexity", "complexity"],
    ["max-depth", "depth"],
    ["max-params", "parameters"],
  ]) {
    const original = eslintInternals.builtinRules.get(name);
    rules[name] = {
      meta: { schema: [] },
      create(context) {
        const proxy = Object.create(context, {
          options: { value: [0] },
          report: {
            value(report) {
              let node = report.node;
              if (field === "depth")
                while (node.parent && !BOUNDARIES.has(node.type))
                  node = node.parent;
              const fact = factFor(node);
              const value =
                report.data[field === "parameters" ? "count" : field];
              fact[field] = Math.max(fact[field], Number(value));
            },
          },
        });
        return original.create(proxy);
      },
    };
  }
  const messages = new Linter({ configType: "flat" }).verify(
    analysis.read(path).slice(offset, offset + parsed.ast.range[1]),
    {
      files: ["**/*.*"],
      languageOptions: { parser: { parseForESLint: () => parsed } },
      linterOptions: {
        noInlineConfig: true,
        reportUnusedDisableDirectives: "off",
      },
      plugins: { facts: { rules } },
      rules: Object.fromEntries(
        Object.keys(rules).map((name) => [`facts/${name}`, "error"]),
      ),
    },
    "source.ts",
  );
  if (messages.length)
    throw new Error(
      `function metrics could not be established: ${messages[0].message}`,
    );
  return [...facts.values()];
}

function functionName(node) {
  return (
    node.id?.name ??
    node.parent?.key?.name ??
    node.parent?.id?.name ??
    "anonymous"
  );
}
