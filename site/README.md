# grafel.app landing site

The landing page is a modular **Astro** static site. `npm run build` emits a
self-contained static bundle to `dist/` (no external requests at runtime) plus
the Cloudflare Pages support files. Hosted free on **Cloudflare Pages**.

## Structure

```
site/
  package.json            # astro dependency; scripts: dev / build / preview / check
  astro.config.mjs        # static output (Astro default)
  tsconfig.json
  public/
    _headers              # security headers + revalidate caching (copied verbatim to dist/)
    404.html              # minimal not-found page (copied verbatim to dist/)
  src/
    layouts/Base.astro    # <!doctype>, <head>, global stylesheet imports, <slot/>
    styles/
      tokens.css          # :root design tokens + light/dark + [data-theme] overrides
      global.css          # base, layout, section, and all component CSS
    components/
      Nav.astro           # sticky .topnav
      Hero.astro          # .hero + the <canvas id="bg-graph"> (loads bg-graph.js)
      Explorer.astro      # grep⇆grafel #explorer widget (loads explorer.js)
      Comparison.astro    # "A companion to grep" comparison grid
      CoverageMatrix.astro# "39 languages…" coverage matrix (loads coverage.js)
      GraphModel.astro    # "59 entity kinds…" graph-model legend
      UnderTheHood.astro  # "Built locally…" engine cards
      GetStarted.astro    # merged install (#start) + wizard steps (loads ui.js)
      Footer.astro        # footer.legal
    scripts/
      explorer.js         # explorer toggle + USE_CASES + narration
      coverage.js         # coverage-matrix + graph-model legend fills
      ui.js               # install-tab switching + reveal-on-scroll observer
      bg-graph.js         # ambient canvas knowledge-graph animation
    pages/
      index.astro         # imports Base + composes the components in DOM order
```

The CSS is imported **globally** (not Astro-scoped) so the rendered cascade is
byte-identical to the original single-file page. The scripts are the original
inline `<script>` logic, split by concern and copied verbatim.

## Develop

```bash
cd site
npm install
npm run dev        # local dev server
npm run build      # emit static bundle to dist/
npm run preview    # preview the built dist/
```

## Deploy (free)

One-time setup — buy `grafel.app` on Cloudflare Registrar (at-cost, ~$12–14/yr; `.app`
is HTTPS-only via HSTS preload, which Cloudflare's automatic SSL covers).

```bash
npm i -g wrangler
wrangler login
cd site
npm run build
wrangler pages deploy dist --project-name grafel
```

Then in the Cloudflare dashboard: **Pages → grafel → Custom domains → add `grafel.app`**.
Because the domain's DNS is already in the same account, the record + cert are
provisioned automatically.

### Git-connected (auto-deploy on push)

Connect this repo as a Pages project with these build settings:

- **Framework preset:** Astro
- **Root directory:** `site`
- **Build command:** `npm run build`
- **Build output directory:** `dist`

Every push to the default branch redeploys.

## Editing

Edit the component/style/script source under `src/`, run `npm run build`, and
redeploy `dist/`. The page is theme-aware (light/dark) and ships no external
requests. `_headers` and `404.html` live in `public/` and are emitted to the
`dist/` root, where Cloudflare Pages expects them.
