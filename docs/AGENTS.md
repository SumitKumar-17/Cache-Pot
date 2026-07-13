# AGENTS.md (docs/)

This subfolder is a separate VitePress project (its own `package.json`, node_modules,
build) documenting the Go module one level up. See the repo root [AGENTS.md](../AGENTS.md)
for the project as a whole; this file is specific to working in `docs/`.

## The one rule that matters here

**`docs/commands/_generated-table.md` is generated, never hand-edited.** It's produced
by `.vitepress/generate-commands.mjs` from `../api/commands.yaml` (the actual source of
truth for the command surface) and pulled into `commands/index.md` via a markdown
`@include`. If you change what a command does, edit `api/commands.yaml`, not this file
— it regenerates automatically as part of `docs:dev`/`docs:build`.

The generator is a **hand-rolled line parser**, not a YAML library — deliberately,
because `api/commands.yaml` has some `summary:` values containing a literal `: `
sequence that strict YAML parsers choke on in an unquoted scalar. If you add a
`summary:` value that needs a colon, either avoid the `: ` sequence (simplest) or check
`generate-commands.mjs`'s parsing logic still handles it before assuming it "just
works."

## Structure

```
docs/
├── index.md                  landing page — the whole product pitch in one page
├── getting-started/          installation, quickstart, configuration, MCP server,
│                             observability, LLM completions — one page per concern
├── commands/                 one narrative page per command family, plus the
│                             generated full table in commands/index.md
├── architecture/             how the Go code is put together (overview, storage
│                             engine, Redis-compatibility honesty page)
└── roadmap/                  the full 7-phase roadmap, kept in sync with ../ROADMAP.md
```

## Conventions

- **Every real command's docs page opens with a `::: tip Phase N — real` callout**;
  every planned one opens with `::: info Planned — Phase N`. Don't mix these up when a
  command flips from planned to real — update the callout, not just the prose.
- **Don't link to roadmap phase-heading anchors** (e.g. `/roadmap/#phase-3-...`). A
  phase's heading text changes when its status flips (`*(planned)*` → `✅`), which
  changes VitePress's auto-generated anchor slug and silently breaks the link — this has
  happened more than once in this project's history. Link to `/roadmap/` (no fragment)
  instead.
- **`cleanUrls: true` in `.vitepress/config.ts` only affects generated internal
  `<a href>` links** — the actual built files on disk still have a `.html` extension
  (confirmed by inspecting `.vitepress/dist/` after a build). The *hosting platform*
  needs its own clean-URL config to map `/foo` → `foo.html`; see `docs/vercel.json`'s
  `cleanUrls: true` for the Vercel-side half of this.
- Every real page in `commands/` or `getting-started/` needs a sidebar entry in
  `.vitepress/config.ts` — an unlinked page is as good as undocumented.
- Keep worked examples honest: if an example depends on the `mock` provider's specific
  behavior (e.g. near-duplicate closeness for `CACHE.SEMANTIC`), say so — a reader
  copy-pasting it with `--embed-provider openai` should not be surprised when real
  embeddings behave differently. This has bitten this project before (see git history
  around the `CACHE.SEMANTIC` threshold-calibration docs update).

## Development

```bash
npm install
npm run docs:dev       # local dev server, regenerates the command table first
npm run docs:build     # production build to .vitepress/dist/
```

Always run a real `docs:build` (not just `docs:dev`) before considering a docs change
done — it's the most reliable smoke test in a non-interactive environment, and it's
what actually catches a broken internal link or an orphaned page.
