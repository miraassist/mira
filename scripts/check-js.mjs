// Syntax check of the page's JavaScript (internal/ui): the inline script of
// index.html and the files in internal/ui/js; a syntax error is named with its
// file and line, and the check fails. Of the page only the guide's Markdown
// functions are run, on the guide (below).
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

// The guide as the page renders it (docs/guide): the page's own Markdown
// functions, taken from index.html and run on both languages. No picture may
// stay as text, every picture's file must exist, and a picture indented under
// a numbered step stays in that step, so the numbers go on.
const fn = (name) => {
  const start = html.search(new RegExp(`^function ${name}\\(`, "m"));
  if (start < 0) throw new Error(`index.html has no function ${name}`);
  const end = html.indexOf("\n}\n", start);
  return html.slice(start, end + 2);
};
const escLine = html.match(/^const esc = .*$/m)[0];
const render = new vm.Script(`${escLine}\nconst url = p => p;\n${fn("inline")}\n${fn("markdown")}\n${fn("media")}\nmarkdown`)
  .runInNewContext({});
const guideDir = join(import.meta.dirname, "..", "docs", "guide");
const guideFail = (msg) => { console.error(`guide: ${msg}`); failed++; };
for (const lang of ["de", "en"]) {
  const out = render(readFileSync(join(guideDir, `${lang}.md`), "utf8"));
  for (const m of out.replace(/<pre[\s\S]*?<\/pre>/g, "").matchAll(/!\[[^\]]*\]\([^)]*\)/g)) {
    guideFail(`${lang}.md shows a picture as text: ${m[0].slice(0, 80)}`);
  }
  for (const m of out.matchAll(/src="\/ui\/guide\/(img\/[^"]+)"/g)) {
    try { readFileSync(join(guideDir, m[1])); } catch { guideFail(`${lang}.md shows ${m[1]}, which does not exist`); }
  }
}
const steps = render("1. One\n   ![First](img/a/one.png)\n2. Two");
if (!/^<ol>\n<li>One<figure>[\s\S]*<\/figure><\/li>\n<li>Two<\/li>\n<\/ol>$/.test(steps)) {
  guideFail(`a picture under a step breaks the list: ${steps}`);
}

if (failed) {
  console.error(`${failed} problem(s) in the page scripts or the guide`);
  process.exit(1);
}
console.log("page scripts: syntax ok; guide renders");
