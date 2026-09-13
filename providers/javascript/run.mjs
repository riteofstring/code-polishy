import { dirname } from "node:path";
import { fileURLToPath } from "node:url";

process.chdir(dirname(fileURLToPath(import.meta.url)));
const { analyze } = await import("./adapter.mjs");
const { boundedText } = await import("./context.mjs");

let input = "";
for await (const chunk of process.stdin) {
  input += chunk;
  if (Buffer.byteLength(input) > 8 * 1024 * 1024)
    throw new Error("request exceeds 8 MiB");
}
try {
  process.stdout.write(`${JSON.stringify(await analyze(JSON.parse(input)))}\n`);
} catch (error) {
  process.stdout.write(
    `${JSON.stringify({ protocolVersion: 3, status: "operational-failure", failure: boundedText(error.message, 4096) })}\n`,
  );
}
