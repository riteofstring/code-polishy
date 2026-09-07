import { dirname } from "node:path";
import { fileURLToPath } from "node:url";

process.chdir(dirname(fileURLToPath(import.meta.url)));
const { analyze } = await import("./adapter.mjs");

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
    `${JSON.stringify({ protocolVersion: 2, status: "operational-failure", failure: error.message.slice(0, 4096) })}\n`,
  );
}
