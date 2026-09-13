import cssScanner from "vscode-css-languageservice/lib/umd/parser/cssScanner.js";

const { Scanner, TokenType } = cssScanner;

export function cssComments(source, offset) {
  const scanner = new Scanner();
  scanner.ignoreComment = false;
  scanner.setSource(source);
  const comments = [];
  for (
    let token = scanner.scan();
    token.type !== TokenType.EOF;
    token = scanner.scan()
  ) {
    if (token.type === TokenType.BadString)
      throw new Error("embedded CSS contains an unterminated string");
    if (token.type !== TokenType.Comment) continue;
    if (!token.text.endsWith("*/"))
      throw new Error("embedded CSS contains an unterminated comment");
    comments.push({
      type: "Block",
      range: [offset + token.offset, offset + token.offset + token.len],
    });
  }
  return comments;
}
