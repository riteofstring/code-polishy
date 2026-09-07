export const FORMAT_OPTIONS = {
  arrowParens: "always",
  bracketSameLine: false,
  bracketSpacing: true,
  embeddedLanguageFormatting: "auto",
  endOfLine: "lf",
  htmlWhitespaceSensitivity: "css",
  jsxSingleQuote: false,
  objectWrap: "preserve",
  printWidth: 80,
  proseWrap: "preserve",
  quoteProps: "as-needed",
  semi: true,
  singleAttributePerLine: false,
  singleQuote: false,
  tabWidth: 2,
  trailingComma: "all",
  useTabs: false,
};

const REACT_HOOKS_RULES = [
  "react-hooks/rules-of-hooks",
  "react-hooks/exhaustive-deps",
];
const JSX_ACCESSIBILITY_RULES = [
  "jsx-a11y/alt-text",
  "jsx-a11y/anchor-has-content",
  "jsx-a11y/anchor-is-valid",
  "jsx-a11y/aria-props",
  "jsx-a11y/aria-role",
  "jsx-a11y/click-events-have-key-events",
  "jsx-a11y/interactive-supports-focus",
  "jsx-a11y/label-has-associated-control",
  "jsx-a11y/no-static-element-interactions",
  "jsx-a11y/tabindex-no-positive",
];

export const TYPECHECK_OPTIONS = {
  noEmit: true,
  noCheck: false,
  emitDeclarationOnly: false,
  composite: false,
  incremental: false,
  tsBuildInfoFile: undefined,
};

export function lintRules(request) {
  const rules = {
    "no-unreachable": "error",
    "no-dupe-else-if": "error",
    "no-duplicate-case": "error",
    "no-constant-binary-expression": "error",
    "no-unsafe-finally": "error",
    "valid-typeof": "error",
    "use-isnan": "error",
    complexity: ["error", request.limits.complexity],
    "max-depth": ["error", request.limits.depth],
    "max-params": ["error", request.limits.parameters],
  };
  const activated = [
    ...(request.activation.reactHooks ? REACT_HOOKS_RULES : []),
    ...(request.activation.jsxAccessibility ? JSX_ACCESSIBILITY_RULES : []),
  ];
  for (const rule of activated) {
    rules[rule] = "error";
  }
  return rules;
}
