/* sync-pdfjs.mjs -- vendor the pinned pdfjs-dist package into
 * assets/pdfjs/ (v0.65, detail-fix ⑥: the built-in PDF viewer).
 *
 * Why vendored files instead of a page bundle: the daemon serves
 * these from the EXE's embedded assets (the xterm precedent), so
 * the single-file build stays fully offline -- every fetch the
 * viewer makes goes to the local daemon, never a CDN. The npm
 * package is the source of truth: package.json pins the exact
 * pdfjs-dist version, and this script copies its runtime files
 * into the repo. Run it after bumping the pin:
 *
 *   npm install            (installs the pinned pdfjs-dist)
 *   node web/sync-pdfjs.mjs
 *
 * What is copied:
 *   legacy/build/pdf.min.mjs        -> pdf.mjs        (library)
 *   legacy/build/pdf.worker.min.mjs -> pdf.worker.mjs (worker)
 *   LICENSE                         -> LICENSE.txt
 *   cmaps/*.bcmap                   -> cmaps/<name>.bcmap.b64
 *   standard_fonts/*                -> standard_fonts/<name>.b64
 *
 * The legacy build is vendored (not build/): it is the widest-
 * compatibility line of the same release -- the terminal runs
 * on shared/public PCs whose Edge may lag. The worker carries
 * pure-JS JBIG2/JPEG2000 decoders, so v4 needs no wasm files.
 *
 * The cMaps and standard fonts are BINARY. The repo's snapshot
 * push channel is UTF-8-text only, so they are stored base64
 * (one .b64 text file per asset, wrapped at 100 columns) and
 * the daemon decodes them at serve time (pdfview.go). The .mjs
 * files are text and are copied verbatim.
 */
import { readdirSync, readFileSync, writeFileSync, mkdirSync, rmSync, existsSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const webDir = dirname(fileURLToPath(import.meta.url));
const repoRoot = join(webDir, '..');
const pkgDir = join(repoRoot, 'node_modules', 'pdfjs-dist');
const outDir = join(repoRoot, 'assets', 'pdfjs');

if (!existsSync(pkgDir)) {
  console.error('node_modules/pdfjs-dist missing -- run npm install first');
  process.exit(1);
}
const pkg = JSON.parse(readFileSync(join(pkgDir, 'package.json'), 'utf8'));
console.log('vendoring pdfjs-dist', pkg.version);

rmSync(outDir, { recursive: true, force: true });
mkdirSync(join(outDir, 'cmaps'), { recursive: true });
mkdirSync(join(outDir, 'standard_fonts'), { recursive: true });

function copyText(src, dst) {
  writeFileSync(join(outDir, dst), readFileSync(join(pkgDir, src)));
  console.log('  text  ', src, '->', dst);
}
function copyB64(srcDir, dstDir) {
  let n = 0, bytes = 0;
  for (const name of readdirSync(join(pkgDir, srcDir)).sort()) {
    const raw = readFileSync(join(pkgDir, srcDir, name));
    const b64 = raw.toString('base64').replace(/.{100}/g, '$&\n');
    writeFileSync(join(outDir, dstDir, name + '.b64'), b64 + '\n');
    n++; bytes += raw.length;
  }
  console.log('  base64', srcDir, '->', dstDir, `(${n} files, ${bytes} bytes raw)`);
}

copyText('legacy/build/pdf.min.mjs', 'pdf.mjs');
copyText('legacy/build/pdf.worker.min.mjs', 'pdf.worker.mjs');
copyText('LICENSE', 'LICENSE.txt');
copyB64('cmaps', 'cmaps');
copyB64('standard_fonts', 'standard_fonts');
writeFileSync(join(outDir, 'VERSION.txt'),
  `pdfjs-dist ${pkg.version} (legacy build), vendored by web/sync-pdfjs.mjs.\n` +
  `cmaps/ and standard_fonts/ files are base64 (.b64) because the repo's\n` +
  `push channel is text-only; pdfview.go decodes them at serve time.\n`);
console.log('done -> assets/pdfjs/');
