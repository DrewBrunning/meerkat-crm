// Issue #476 (WEB-02): the sw-upgrade test harness HTTP server.
//
// A service-worker upgrade can only be staged if the *same origin* serves a
// different build on demand: the browser decides whether a new worker exists
// by byte-comparing /service-worker.js, and only then re-runs precache. This
// server is that origin. It serves one of the two fixture builds (or the
// "broken deploy" poison worker) and lets the tests swap the active build
// between navigations, which is what turns a one-build static server into a
// two-build upgrade rig.
//
// It deliberately mirrors the production nginx behaviour that service-worker
// correctness depends on:
//   - /service-worker.js and HTML are served with no-cache (never stale),
//   - hashed assets are served as immutable,
//   - /_recovery.html and /_recovery.js are served with no-store,
//   - a missing asset extension 404s (no SPA fallback), while an unknown
//     extension-less route falls back to index.html like nginx's try_files.
//
// The harness control surface lives under /__swtest/* and is only ever called
// from Node (Playwright's request fixture), never from a page -- a page's
// requests would be subject to whatever service worker is installed, and the
// control surface must not be.

import { readFile } from 'node:fs/promises';
import http from 'node:http';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

import { FIXTURE_DIRS, FIXTURE_LABELS, FIXTURE_VERSIONS, precacheableFiles } from './fixtures.mjs';

const __dirname = path.dirname(fileURLToPath(import.meta.url));

export const DEFAULT_PORT = Number(process.env.SW_TEST_PORT || 7300);
export const HARNESS_STATUS_PATH = '/__swtest/status';
export const HARNESS_ACTIVE_PATH = '/__swtest/active';
export const HARNESS_HEALTH_PATH = '/__swtest/health';

const POISON_WORKER_PATH = path.join(__dirname, 'poison-worker.js');

const CONTENT_TYPES = {
  '.html': 'text/html; charset=utf-8',
  '.js': 'application/javascript; charset=utf-8',
  '.mjs': 'application/javascript; charset=utf-8',
  '.css': 'text/css; charset=utf-8',
  '.json': 'application/json; charset=utf-8',
  '.webmanifest': 'application/manifest+json; charset=utf-8',
  '.txt': 'text/plain; charset=utf-8',
  '.xml': 'text/xml; charset=utf-8',
  '.png': 'image/png',
  '.jpg': 'image/jpeg',
  '.jpeg': 'image/jpeg',
  '.gif': 'image/gif',
  '.ico': 'image/x-icon',
  '.svg': 'image/svg+xml',
  '.woff': 'font/woff',
  '.woff2': 'font/woff2',
  '.ttf': 'font/ttf',
  '.eot': 'application/vnd.ms-fontobject',
};

const ASSET_EXTENSION = /\.(js|css|png|jpe?g|gif|ico|svg|woff2?|ttf|eot)$/;
const PROFILES = ['a', 'b', 'poison'];

function profileBuildDir(profile) {
  if (profile === 'poison') return FIXTURE_DIRS.a;
  return FIXTURE_DIRS[profile];
}

export class SwUpgradeServer {
  constructor(buildDirs = FIXTURE_DIRS) {
    this.buildDirs = buildDirs;
    this.profile = 'a';
    this.fileSets = {};
    for (const label of FIXTURE_LABELS) {
      this.fileSets[label] = precacheableFiles(buildDirs[label]);
    }
    this.poisonWorker = readFile(POISON_WORKER_PATH, 'utf8');
    // Issue #475: optional test override for the /health the app's stale-
    // client detector polls. null = advertise the active build's own release
    // identity (compatible with that build); an override lets a spec raise
    // min_client_version / api_contract_version or fail /health entirely
    // without swapping the served build.
    this.healthOverride = null;
  }

  get activeProfile() {
    return this.profile;
  }

  async statusPayload() {
    const builds = {};
    for (const label of FIXTURE_LABELS) {
      builds[label] = {
        dir: this.buildDirs[label],
        files: await this.fileSets[label],
      };
    }
    return {
      active: this.profile,
      builds,
      recoveryPaths: ['/_recovery.html', '/_recovery.js'],
      harnessPaths: [HARNESS_STATUS_PATH, HARNESS_ACTIVE_PATH, HARNESS_HEALTH_PATH],
    };
  }

  setProfile(profile) {
    if (!PROFILES.includes(profile)) {
      throw new Error(`unknown profile: ${profile}`);
    }
    this.profile = profile;
  }

  // The /health body the stale-client detector reads. By default it advertises
  // the active build's own release identity (fixture "a" is 0.6.8, "b" is
  // 0.6.10) with contract "v1" and no floor — i.e. exactly what a build that
  // matches that release expects to see, so existing specs are unaffected.
  // `healthOverride` fields replace the base values; `{ error: true }` makes
  // /health fail, to prove the detector fails open.
  healthPayload() {
    const label = this.profile === 'poison' ? 'a' : this.profile;
    const override = this.healthOverride ?? {};
    return {
      status: 'healthy',
      timestamp: new Date().toISOString(),
      database: { status: 'healthy', response_time_ms: 1 },
      version: FIXTURE_VERSIONS[label],
      api_contract_version: 'v1',
      ...override,
    };
  }

  setHealthOverride(override) {
    this.healthOverride = override;
  }

  resetHealthOverride() {
    this.healthOverride = null;
  }

  async serveHealth(res) {
    const override = this.healthOverride ?? {};
    if (override.error) {
      // An unreachable/failing /health is a routine event the app must fail
      // open against; a non-2xx is the simplest way to stage it.
      this.sendJson(res, 503, {
        status: 'unhealthy',
        database: { status: 'unhealthy', response_time_ms: 0 },
      });
      return;
    }
    this.sendJson(res, 200, this.healthPayload());
  }

  async handle(req, res) {
    const url = new URL(req.url ?? '/', 'http://localhost');
    const method = req.method ?? 'GET';
    const pathname = decodeURIComponent(url.pathname);

    // ---- Harness control surface (Node-only) -------------------------------
    if (pathname === HARNESS_STATUS_PATH && method === 'GET') {
      this.sendJson(res, 200, await this.statusPayload());
      return;
    }
    if (pathname === HARNESS_ACTIVE_PATH && method === 'POST') {
      const body = await readJsonBody(req);
      const profile = body && body.build;
      if (!PROFILES.includes(profile)) {
        this.sendJson(res, 400, {
          error: `build must be one of ${PROFILES.join('|')}, got ${profile}`,
        });
        return;
      }
      this.setProfile(profile);
      this.sendJson(res, 200, { active: this.profile });
      return;
    }
    if (pathname === HARNESS_HEALTH_PATH && method === 'POST') {
      const body = (await readJsonBody(req)) ?? {};
      if (body.reset) {
        this.resetHealthOverride();
      } else {
        this.setHealthOverride(body);
      }
      this.sendJson(res, 200, { health: this.healthPayload() });
      return;
    }
    if (pathname === HARNESS_HEALTH_PATH && method === 'GET') {
      this.sendJson(res, 200, { health: this.healthPayload() });
      return;
    }

    if (method !== 'GET' && method !== 'HEAD') {
      res.writeHead(405, { 'Content-Type': 'text/plain' });
      res.end('method not allowed');
      return;
    }

    // ---- Content -----------------------------------------------------------
    // Issue #475: the app's stale-client detector polls /health on load and on
    // tab focus; serve the harness's contract payload so that polling has
    // something real to read (and can be told to fail, for the fail-open spec).
    if (pathname === '/health') {
      await this.serveHealth(res);
      return;
    }

    // A poisoned (broken) deploy serves its own worker script in place of the
    // build's. Everything else comes from the active fixture's build dir.
    const swBytes =
      this.profile === 'poison' && pathname === '/service-worker.js'
        ? await this.poisonWorker
        : undefined;

    if (swBytes !== undefined) {
      this.writeBytes(res, 200, swBytes, 'application/javascript; charset=utf-8', {
        'Cache-Control': 'no-cache',
      });
      return;
    }

    const buildDir = profileBuildDir(this.profile);
    await this.serveFileOrFallback(res, pathname, buildDir);
  }

  async serveFileOrFallback(res, pathname, buildDir) {
    const filePath = resolveInside(buildDir, pathname);

    // Read the file directly -- one operation, no separate existence/type
    // check that could race with the read (a stale check is exactly the bug
    // the file is serving against). A missing file, a path that is a
    // directory, or a transient removal all surface here as "not a readable
    // file" and fall through to the 404/SPA-fallback logic below.
    let body;
    let ext = '';
    if (filePath !== null) {
      try {
        body = await readFile(filePath);
        ext = path.extname(filePath).toLowerCase();
      } catch {
        body = undefined;
      }
    }

    if (body !== undefined) {
      const contentType = CONTENT_TYPES[ext] ?? 'application/octet-stream';
      const headers = {};

      if (pathname === '/service-worker.js' || ext === '.html') {
        // The worker script and the app shell must never be served stale; the
        // browser checks the former before every update.
        headers['Cache-Control'] = 'no-cache';
      } else if (pathname === '/_recovery.html' || pathname === '/_recovery.js') {
        headers['Cache-Control'] = 'no-store';
      } else if (ASSET_EXTENSION.test(pathname)) {
        // Hashed build assets and static public files: content-addressed or
        // effectively versioned, mirroring nginx's 1-year immutable rule.
        headers['Cache-Control'] = 'public, max-age=31536000, immutable';
      }

      this.writeBytes(res, 200, body, contentType, headers);
      return;
    }

    // Missing asset-extension path: 404 (nginx's regex asset locations have no
    // SPA fallback). Everything else (a client-side route) falls back to the
    // app shell, like nginx's try_files ... /index.html.
    if (ASSET_EXTENSION.test(pathname)) {
      res.writeHead(404, { 'Content-Type': 'text/plain' });
      res.end('not found');
      return;
    }

    // The SPA shell is itself read with a single operation (no prior check).
    try {
      const indexHtml = await readFile(path.join(buildDir, 'index.html'));
      this.writeBytes(res, 200, indexHtml, CONTENT_TYPES['.html'], {
        'Cache-Control': 'no-cache',
      });
      return;
    } catch {
      // no shell -- fixture missing, reported below
    }

    res.writeHead(500, { 'Content-Type': 'text/plain' });
    res.end('fixture build missing -- run "yarn sw:fixtures"');
  }

  sendJson(res, status, payload) {
    this.writeBytes(res, status, JSON.stringify(payload), 'application/json; charset=utf-8', {
      'Cache-Control': 'no-store',
    });
  }

  writeBytes(res, status, bytes, contentType, headers = {}) {
    const body = Buffer.isBuffer(bytes) ? bytes : Buffer.from(bytes);
    const allHeaders = {
      'Content-Type': contentType,
      'Content-Length': String(body.length),
      ...headers,
    };
    res.writeHead(status, allHeaders);
    if (res.req.method !== 'HEAD') {
      res.end(body);
    } else {
      res.end();
    }
  }
}

function readJsonBody(req) {
  return new Promise((resolve, reject) => {
    let raw = '';
    req.setEncoding('utf8');
    req.on('data', (chunk) => {
      raw += chunk;
      if (raw.length > 1024 * 64) {
        reject(new Error('request body too large'));
        req.destroy();
      }
    });
    req.on('end', () => {
      if (!raw) {
        resolve(null);
        return;
      }
      try {
        resolve(JSON.parse(raw));
      } catch {
        reject(new Error('invalid JSON body'));
      }
    });
    req.on('error', reject);
  });
}

function resolveInside(buildDir, pathname) {
  const decoded = decodeURIComponent(pathname);
  const candidate = path.normalize(path.join(buildDir, decoded));
  const rootReal = path.resolve(buildDir);
  const candidateReal = path.resolve(candidate);
  if (candidateReal !== rootReal && !candidateReal.startsWith(`${rootReal}${path.sep}`)) {
    return null;
  }
  return candidateReal;
}

export async function createSwUpgradeServer(port) {
  const server = new SwUpgradeServer();
  const httpServer = http.createServer((req, res) => {
    server.handle(req, res).catch((err) => {
      res.writeHead(500, { 'Content-Type': 'text/plain' });
      res.end(`harness error: ${String(err)}`);
    });
  });

  await new Promise((resolve, reject) => {
    httpServer.once('error', reject);
    httpServer.listen(port, '127.0.0.1', () => resolve());
  });

  return httpServer;
}
