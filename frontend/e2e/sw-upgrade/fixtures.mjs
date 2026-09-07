// Issue #476 (WEB-02): builds the two "release" fixtures the service-worker
// upgrade suite stages. A service worker only updates when the server's
// /service-worker.js is byte-different from the installed one, so the suite
// needs two REAL production builds ("a" then "b") whose emitted files differ.
//
// Both fixtures are built from the current source. They differ only because
// vite.config.ts appends the MYCORRHIZAL_SW_FIXTURE label to the entry chunk's
// filename when that env var is set -- which changes index.html (it references
// the entry), hence the injected workbox precache manifest, hence the
// service-worker.js bytes. Unchanged vendor/lazy chunks keep their
// content-hashed names across both fixtures, exactly like a real deploy that
// ships an unchanged chunk.
//
// The fixtures are build artifacts and are never committed (see
// frontend/.gitignore). They are rebuilt whenever the stamped inputs change or
// the fixture dirs are missing, and can be forced with --force (npm run
// sw:fixtures -- --force).

import { spawnSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { mkdir, readdir, readFile, rm, writeFile } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));

export const FRONTEND_DIR = path.resolve(__dirname, '../..');
export const FIXTURES_DIR = path.join(__dirname, 'fixtures');
export const FIXTURE_LABELS = ['a', 'b'];

export const FIXTURE_DIRS = {
  a: path.join(FIXTURES_DIR, 'a'),
  b: path.join(FIXTURES_DIR, 'b'),
};

// Issue #475 (WEB-01): each fixture also embeds the release version the web
// client compares against /health. "a" is an older release than "b" so the
// stale-client spec can stage the scenario that matters — an open tab running
// "a" while the server has deployed "b" and raised its min_client_version
// above "a". The value is stamped into the bundle via VITE_APP_VERSION below,
// the same way docker-publish.yml stamps real release builds.
export const FIXTURE_VERSIONS = {
  a: '0.6.8',
  b: '0.6.10',
};

const VITE_JS = path.join(FRONTEND_DIR, 'node_modules', 'vite', 'bin', 'vite.js');
const STAMP_FILE = path.join(FIXTURES_DIR, '.fixture-stamp');

// Inputs that a fixture rebuild must track: if any of these change, the
// emitted chunks/HTML may change and the cached fixtures would silently drift
// from the source they claim to represent. Whether each entry is a directory
// is known statically (src/ and public/ are directories; the rest are files),
// so the stamp code never needs an existence check ahead of a read.
const STAMPED_INPUTS = [
  { rel: 'src', isDirectory: true },
  { rel: 'public', isDirectory: true },
  { rel: 'index.html', isDirectory: false },
  { rel: 'vite.config.ts', isDirectory: false },
  { rel: 'package.json', isDirectory: false },
];

// Reads a file that may or may not exist (an input the fixture build does not
// care about is treated as empty). Using a single read rather than a separate
// existence check avoids the check-then-read race a stale read could produce.
async function readIfPresent(file) {
  try {
    return await readFile(file);
  } catch {
    return Buffer.alloc(0);
  }
}

// Recursively lists files under `dir`. A missing directory is simply empty:
// readdir is the single source of truth, no prior existence check needed.
async function listFiles(dir) {
  const out = [];
  async function walk(current) {
    let entries;
    try {
      entries = await readdir(current, { withFileTypes: true });
    } catch {
      return;
    }
    for (const entry of entries) {
      if (entry.name === 'node_modules') continue;
      const full = path.join(current, entry.name);
      if (entry.isDirectory()) {
        await walk(full);
      } else {
        out.push(path.relative(dir, full));
      }
    }
  }
  await walk(dir);
  return out.sort();
}

export async function computeSourceStamp() {
  const hash = createHash('sha256');
  for (const { rel, isDirectory } of STAMPED_INPUTS) {
    const full = path.join(FRONTEND_DIR, rel);
    hash.update(`${rel}\0`);
    if (isDirectory) {
      const files = await listFiles(full);
      for (const file of files) {
        hash.update(`${rel}/${file}\0`);
        hash.update(await readIfPresent(path.join(full, file)));
      }
    } else {
      hash.update(await readIfPresent(full));
    }
  }
  return hash.digest('hex');
}

export async function fixturesNeedRebuild() {
  for (const label of FIXTURE_LABELS) {
    try {
      await readFile(path.join(FIXTURE_DIRS[label], 'index.html'));
    } catch {
      return true;
    }
  }
  let previous;
  try {
    previous = (await readFile(STAMP_FILE, 'utf8')).trim();
  } catch {
    return true;
  }
  return previous !== (await computeSourceStamp());
}

async function buildFixture(label, outDir) {
  console.log(`[sw-upgrade] building fixture "${label}" -> ${outDir}`);
  await rm(outDir, { recursive: true, force: true });
  await mkdir(outDir, { recursive: true });

  const result = spawnSync(
    process.execPath,
    [VITE_JS, 'build', '--outDir', outDir, '--emptyOutDir'],
    {
      cwd: FRONTEND_DIR,
      env: {
        ...process.env,
        MYCORRHIZAL_SW_FIXTURE: label,
        // Issue #475: stamp the release version this fixture claims to be, so
        // the stale-client detector is live in the fixture builds and the
        // WEB-01 spec can drive a below-floor reload.
        VITE_APP_VERSION: FIXTURE_VERSIONS[label],
      },
      stdio: 'inherit',
    },
  );
  if (result.error) throw result.error;
  if (result.status !== 0) {
    throw new Error(`fixture build "${label}" failed with exit code ${result.status}`);
  }
}

// Ensures both fixtures exist and match the current source. Returns the labels
// that were (re)built. `force` skips the stamp check.
export async function ensureFixtures(force = false) {
  const need = force || (await fixturesNeedRebuild());
  if (!need) {
    console.log('[sw-upgrade] fixtures are up to date');
    return [];
  }

  const built = [];
  for (const label of FIXTURE_LABELS) {
    await buildFixture(label, FIXTURE_DIRS[label]);
    built.push(label);
  }

  await writeFile(STAMP_FILE, await computeSourceStamp());
  console.log('[sw-upgrade] fixtures ready');
  return built;
}

// Builds every file the workbox precache should contain -- i.e. the set a
// freshly installed worker caches -- so the harness can expose each fixture's
// expected precache set to the tests. vite-plugin-pwa's injectManifest globs
// js/mjs/css/html only (icons, fonts and other public files are runtime
// requests, not precache entries), and never precaches the worker script
// itself, the /_recovery.* escape-hatch page, or the /asset-skew.js bootstrap
// (globIgnores in vite.config.ts).
export async function precacheableFiles(buildDir) {
  const files = await listFiles(buildDir);
  return files
    .map((f) => f.split(path.sep).join('/'))
    .filter((f) => {
      if (!/\.(js|mjs|css|html)$/.test(f)) return false;
      if (f === 'service-worker.js' || f === 'service-worker.mjs') return false;
      if (f === '_recovery.html' || f === '_recovery.js') return false;
      if (f === 'asset-skew.js') return false;
      if (f.startsWith('workbox-')) return false;
      if (f.endsWith('.map')) return false;
      return true;
    })
    .map((f) => `/${f}`);
}

if (import.meta.url === `file://${process.argv[1]}`) {
  const force = process.argv.includes('--force');
  ensureFixtures(force).then(
    (built) => {
      console.log(`[sw-upgrade] fixtures ready (built: ${built.join(', ') || 'none'})`);
    },
    (err) => {
      console.error(err);
      process.exit(1);
    },
  );
}
