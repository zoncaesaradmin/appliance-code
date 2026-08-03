import { execFileSync } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

const specPath = "../../docs/openapi/control-plane-v1.yaml";
const generatedPath = "src/openapi-types.ts";
const tmpDir = mkdtempSync(join(tmpdir(), "appliance-ui-openapi-"));
const tmpPath = join(tmpDir, "openapi-types.ts");

try {
  execFileSync("./node_modules/.bin/openapi-typescript", [specPath, "-o", tmpPath], {
    stdio: "pipe"
  });

  const expected = readFileSync(tmpPath, "utf8");
  const actual = readFileSync(generatedPath, "utf8");
  if (actual !== expected) {
    console.error(
      `${generatedPath} is out of date. Run: npm run openapi:types`
    );
    process.exit(1);
  }
} finally {
  rmSync(tmpDir, { force: true, recursive: true });
}
