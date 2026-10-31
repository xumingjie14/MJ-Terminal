# web/ — MJ Terminal 前端源码（v0.58 起）

The desktop page used to be one hand-written `index.html` (~5,500
lines). Since v0.58 it is **generated**: the sources live here as
TypeScript modules and build back into the single self-contained
`index.html` at the repo root — the exact file the Go daemon embeds
(`//go:embed index.html`) and serves. Runtime behaviour, DOM ids and
classes are unchanged; only the source organisation moved.

## Layout

- `shell.html` — the page template: all markup and CSS verbatim,
  with slots where the build injects code. Two things in it are
  deliberately NOT TypeScript:
  - the `const APPVER='__MJ_APPVER__';` line at the top of script
    block 1 — the daemon substitutes the placeholder byte-exact at
    serve time and a Go test pins its shape;
  - the lockscreen markup sitting between script blocks 1 and 2.
- `src/b1/`, `src/b2/`, `src/b3/` — the modules behind the page's
  three classic script blocks, in original evaluation order
  (`main.ts` per bundle). Cross-bundle globals are installed by
  each bundle's `expose.ts` as live accessors on `window` and
  declared ambiently in `src/globals.d.ts`.
- `src/pure/` — the five marker-delimited pure segments
  (dockmag / ncgroup / launchpad / spotlight / filesview). The node
  test suites extract these verbatim from the BUILT page between
  `/* === X pure (node-testable) === */` markers, so they are
  transpiled standalone (no bundling, no renaming) and inlined
  between their markers as classic page globals.
- `build.mjs` — the build: tsc is NOT part of it (typecheck is a
  separate step); it transpiles the pure segments, bundles each
  block with esbuild (IIFE, no minify), assembles the shell, and
  asserts the artifact's invariants (exactly 3 script blocks, the
  APPVER placeholder present once in block 1, all marker pairs).

## Commands (repo root)

```sh
npm ci            # once: install the pinned toolchain
npm run build     # rebuild ../index.html from web/
npm run verify    # typecheck + build + all seven JS suites
                  # against the BUILT artifact
```

## Artifact policy

The built `index.html` is COMMITTED (the Go embed needs it in the
repo). CI's verify job rebuilds the frontend and fails if the
committed artifact differs from a fresh build — a stale artifact
can never ship silently. If you edit anything under `web/`, run
`npm run build` and commit the regenerated `index.html` with it.

## Toolchain (pinned in package.json / package-lock.json)

- typescript 5.9.3 — `tsc --noEmit` gate. tsconfig is deliberately
  non-strict for this legacy-shaped codebase (DOM surfaces carry
  `any` index signatures in globals.d.ts); the check that matters
  here — undefined names / broken module wiring — is fully on.
- esbuild 0.28.2 — bundling only; output is readable, unminified,
  and deterministic (same sources ⇒ same bytes).
- node 24 (local and CI).
