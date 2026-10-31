/* MJ Terminal page build (v0.58).
 *
 * Assembles the single self-contained page the daemon embeds
 * (../index.html) from:
 *   - web/shell.html        markup + CSS + three empty script slots
 *   - web/src/b1|b2|b3      TypeScript modules -> three esbuild
 *                           IIFE bundles (one per script slot, the
 *                           same load/eval order as the page has
 *                           always had)
 *   - web/src/pure/*.ts     the five node-testable pure-function
 *                           segments, transformed WITHOUT bundling
 *                           and inlined between their marker
 *                           comments, where they stay page globals
 *
 * The build is deterministic: pinned esbuild (package-lock) over
 * pinned sources gives byte-identical output -- CI rebuilds and
 * diffs the result against the committed ../index.html, so a
 * stale artifact can never ship.
 *
 * Run from the repo root:  npm run build
 * (typecheck first with:   npm run typecheck)
 */
import { buildSync, transformSync } from 'esbuild';
import { readFileSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const webDir = dirname(fileURLToPath(import.meta.url));
const repoRoot = join(webDir, '..');
const TARGET = 'es2022';

function bundle(entry) {
  const out = buildSync({
    entryPoints: [join(webDir, 'src', entry)],
    bundle: true,
    format: 'iife',
    target: TARGET,
    charset: 'utf8',
    write: false,
    logLevel: 'warning',
  });
  return out.outputFiles[0].text;
}

function pure(name, marker) {
  const src = readFileSync(join(webDir, 'src', 'pure', name + '.ts'), 'utf8');
  const { code } = transformSync(src, { loader: 'ts', target: TARGET, charset: 'utf8' });
  return '/* === ' + marker + ' pure (node-testable) === */\n' + code.trim() + '\n/* === end ' + marker + ' pure === */';
}

let shell = readFileSync(join(webDir, 'shell.html'), 'utf8');
const slots = {
  '/*__MJ_PURE_DOCKMAG__*/': () => pure('dockmag', 'dockmag'),
  '/*__MJ_PURE_NCGROUP__*/': () => pure('ncgroup', 'ncgroup'),
  '/*__MJ_PURE_SPOT__*/': () => pure('spot', 'spot'),
  '/*__MJ_PURE_LP__*/': () => pure('lp', 'lp'),
  '/*__MJ_PURE_FV__*/': () => pure('fv', 'fv'),
  '/*__MJ_BUNDLE_1__*/': () => bundle('b1/main.ts'),
  '/*__MJ_BUNDLE_2__*/': () => bundle('b2/main.ts'),
  '/*__MJ_BUNDLE_3__*/': () => bundle('b3/main.ts'),
};
for (const [token, fill] of Object.entries(slots)) {
  if (!shell.includes(token)) throw new Error('shell is missing slot ' + token);
  shell = shell.replace(token, () => fill());
}
if (shell.includes('/*__MJ_')) throw new Error('an MJ slot token survived the build');

const html = shell;

/* Post-build invariants -- the same facts the daemon and the node
   suites depend on:
   - exactly three classic script blocks (deskboot/desk56 extract
     them with /<script>([\s\S]*?)<\/script>/g);
   - the daemon's APPVER placeholder survives byte-exact in the
     first block (main.go bytes.ReplaceAll substitutes it at
     serve time);
   - all five pure marker pairs are present for the extractive
     suites. */
const blocks = [...html.matchAll(/<script>([\s\S]*?)<\/script>/g)].map(m => m[1]);
if (blocks.length !== 3) throw new Error('built page must contain exactly 3 script blocks, got ' + blocks.length);
if (!blocks[0].includes('__MJ_APPVER__')) throw new Error('APPVER placeholder missing from built block 1');
for (const mk of ['dockmag', 'ncgroup', 'spot', 'lp', 'fv']) {
  if (!html.includes('/* === ' + mk + ' pure (node-testable) === */')) throw new Error('pure marker missing: ' + mk);
}

writeFileSync(join(repoRoot, 'index.html'), html);
console.log('built index.html:', html.length, 'bytes, 3 script blocks OK, APPVER placeholder OK');
