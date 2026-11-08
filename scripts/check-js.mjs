// Syntax check of the page's JavaScript (internal/ui): the inline script of
// index.html and the files in internal/ui/js. Nothing is run; a syntax error
// is named with its file and line, and the check fails.
//
//   node scripts/check-js.mjs
import { readFileSync, readdirSync } from "node:fs";
import { spawnSync } from "node:child_process";
import { join } from "node:path";
import vm from "node:vm";

const ui = join(import.meta.dirname, "..", "internal", "ui");
let failed = 0;

// The inline scripts of the page, compiled as classic scripts.
const page = join(ui, "index.html");
const html = readFileSync(page, "utf8");
for (const m of html.matchAll(/<script>([\s\S]*?)<\/script>/g)) {
  const line = html.slice(0, m.index).split("\n").length - 1;
  try {
    new vm.Script(m[1], { filename: page, lineOffset: line });
  } catch (e) {
    console.error(`${page}: ${e.stack.split("\n").slice(0, 3).join("\n")}`);
    failed++;
  }
}

// The script files; Node tells a module by its import and export.
const dir = join(ui, "js");
for (const name of readdirSync(dir).filter((n) => n.endsWith(".js"))) {
  const r = spawnSync(process.execPath, ["--check", join(dir, name)], { encoding: "utf8" });
  if (r.status !== 0) {
    console.error(r.stderr.trim());
    failed++;
  }
}

if (failed) {
  console.error(`${failed} script(s) with a syntax error`);
  process.exit(1);
}
console.log("page scripts: syntax ok");
