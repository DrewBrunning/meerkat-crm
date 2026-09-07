import react from '@vitejs/plugin-react';
import browserslistToEsbuild from 'browserslist-to-esbuild';
import { defineConfig } from 'vite';
import { VitePWA } from 'vite-plugin-pwa';

// Issue #476 (WEB-02) test plumbing. The service-worker upgrade e2e suite
// (frontend/e2e/sw-upgrade/, playwright.sw.config.ts) stages two REAL
// production builds -- "build A" then "build B" -- and drives an upgrade
// between them, which only happens if the two builds emit different files.
// The fixtures are built from the same source, so only the emitted *names*
// differ: MYCORRHIZAL_SW_FIXTURE=<label> appends the label to the entry
// chunk's filename, which changes index.html (it references the entry) and
// therefore the injected precache manifest and service-worker.js bytes --
// enough for a browser to detect a genuinely new worker. Unset in every real
// build, this is a no-op. See vite.config.ts's entryFileNames below.
const swFixtureLabel = process.env.MYCORRHIZAL_SW_FIXTURE ?? '';

// https://vite.dev/config/
export default defineConfig({
  plugins: [
    react(),
    // InjectManifest strategy to match CRA's Workbox setup: the service worker
    // is a hand-written file (src/service-worker.ts) whose precache manifest is
    // injected at build time. Registration stays manual via
    // src/serviceWorkerRegistration.ts (index.tsx calls register()), so the
    // plugin must not inject its own registerSW() call into index.html.
    VitePWA({
      strategies: 'injectManifest',
      srcDir: 'src',
      filename: 'service-worker.ts',
      injectRegister: false,
      // The web manifest lives in public/manifest.json (dark/light themed
      // icons) and is linked from index.html directly -- let the plugin not
      // generate a competing one.
      manifest: false,
      // The app legitimately ships the entire @mdi/js icon set (2.7MB): the
      // free-text link-field-type icon input (T43) does a dynamic lookup
      // against the namespace import, so it cannot be tree-shaken. Workbox's
      // default 2MiB precache limit would fail the build on that one chunk
      // (vite-plugin-pwa defaults throwMaximumFileSizeToCacheInBytes to true,
      // unlike workbox-build's warn-only default that CRA used). Raise it to
      // cover the largest chunk with headroom rather than drop it from the
      // precache, which would break offline/Web Push.
      injectManifest: {
        maximumFileSizeToCacheInBytes: 4 * 1024 * 1024,
        // Issue #476 (WEB-02): the /_recovery.* escape-hatch page must NEVER
        // enter the precache. A broken worker would otherwise serve a cached
        // (possibly stale) copy of the one page whose whole purpose is to
        // recover from a broken cache. The page is excluded from the app's
        // navigation interception by the /_ prefix check in service-worker.ts
        // and reaches the network directly; excluding it from the manifest
        // keeps the precache route out of the way as well.
        //
        // Issue #477 (WEB-03): /asset-skew.js is excluded for the same reason
        // in spirit -- it is the recovery bootstrap for a stale index.html
        // whose chunks the current deploy no longer serves, and precaching it
        // would let an old worker serve a stale copy of the one script whose
        // job is to notice staleness. It is a stable URL (never content-hashed)
        // and is served no-cache by nginx, so every load gets the current one.
        globIgnores: ['**/_recovery.html', '**/_recovery.js', '**/asset-skew.js'],
      },
    }),
  ],
  server: {
    // Matches .claude/launch.json's frontend-dev config and the e2e suite's
    // hardcoded base URL (frontend/e2e/global-setup.ts).
    port: 7300,
  },
  build: {
    // COMPAT-01 (issue #472): the supported-browser floor is declared once,
    // in package.json's "browserslist" (grounded in Web Push's Safari 16.4+
    // requirement -- see docs/development/supported-runtime-matrix.md), and
    // read here so it actually constrains the build instead of living only
    // in prose. esbuild lowers/rejects syntax the declared floor can't run.
    target: browserslistToEsbuild(),
    // Keep CRA's output directory so the Dockerfile COPY paths and .gitignore
    // entries stay unchanged.
    outDir: 'build',
    rollupOptions: {
      output: {
        // CRA (webpack) split vendor code out of the app bundle; Vite's default
        // single chunk is one ~4MB file, which both defeats HTTP caching and
        // blows past Workbox's 2MiB precache limit (vite-plugin-pwa fails the
        // build on it). Restore a coarse vendor split. Vite 8's Rolldown
        // bundler only accepts a function here (the Rollup object form that
        // mapped a chunk to bare module names was removed), so match resolved
        // package names against the same groups.
        manualChunks(id) {
          if (!id.includes('/node_modules/')) return undefined;
          const rest = id.slice(id.indexOf('/node_modules/') + '/node_modules/'.length);
          const pkg = rest.startsWith('@')
            ? rest.split('/').slice(0, 2).join('/')
            : rest.split('/')[0];
          for (const [chunk, packages] of VENDOR_CHUNKS) {
            if (packages.includes(pkg)) return chunk;
          }
          return undefined;
        },
        // Issue #476: only active when MYCORRHIZAL_SW_FIXTURE is set (see the
        // comment at the top of this file). Labels the SPA entry chunk so two
        // otherwise-identical fixture builds emit distinct files. Only the
        // entry is labelled: vendor/lazy chunks keep their content-hashed
        // names (which stay the same across fixtures, exactly like unchanged
        // chunks in a real deploy), and the vite-plugin-pwa service-worker
        // build overrides its own output filename, so this never renames
        // /service-worker.js.
        ...(swFixtureLabel
          ? {
              entryFileNames(chunk) {
                return chunk.name === 'index'
                  ? `assets/${chunk.name}-swf${swFixtureLabel}-[hash].js`
                  : `assets/${chunk.name}-[hash].js`;
              },
            }
          : {}),
      },
    },
  },
});

const VENDOR_CHUNKS: ReadonlyArray<readonly [string, readonly string[]]> = [
  ['react-vendor', ['react', 'react-dom', 'react-router']],
  ['mui-core', ['@mui/material', '@mui/lab', '@emotion/react', '@emotion/styled']],
  ['mui-icons', ['@mui/icons-material']],
  ['mdi', ['@mdi/react', '@mdi/js']],
  ['i18n-vendor', ['i18next', 'react-i18next', 'i18next-browser-languagedetector']],
  ['graph-vendor', ['react-force-graph-2d', 'd3-force']],
];
