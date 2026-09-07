// Issue #477 (WEB-03): the asset-skew bootstrap.
//
// A deployment is not atomic: between "the new build is being served" and
// "every client has the new index.html", a browser can hold a document that
// references hashed build assets the server no longer serves. Concretely, a
// client that loads a stale index.html (an in-flight response, an HTTP cache
// revalidation that straddled the swap, or a fresh visit while nginx already
// swapped) asks for entry/vendor chunks whose names only the previous build
// knew -- and the new image deleted them. Each of those requests 404s, the ES
// module graph fails to evaluate, and the page is left as a blank white screen
// with zero indication why: the app's own JS (and so its ErrorBoundary, its
// service-worker plumbing and its stale-client detector) never ran.
//
// This file is the one piece of the app that still runs in that state, so it
// has to live OUTSIDE the bundle (it is a stable, un-hashed /asset-skew.js in
// public/, referenced from index.html <head> before the module entry) and be
// fully self-contained -- no imports, no framework. Its whole purpose is to
// notice a document whose module assets cannot load, wait a moment for an
// in-flight deploy to finish, then reload onto whatever the server serves now.
//
// The recovery is deliberately bounded. After a few consecutive broken loads
// (an interrupted deploy that is genuinely stuck, not just slow) the script
// stops reloading and renders a plain message with a retry button, so a user
// is never spun in a reload loop and never left on a blank page with no way
// forward. Draft data is safe by construction: the app never mounted in a
// broken load, so there is no form state to lose, and #557's per-surface
// drafts live in sessionStorage, which a reload does not clear.

(() => {
  // Only assets under /assets/ are content-hashed build output. A 404 on any
  // other resource (favicon, manifest, fonts) is not a build-skew signal and
  // must not trigger a reload.
  var ASSET_PREFIX = '/assets/';

  // How many consecutive broken loads get an automatic reload before the
  // script stops and shows the manual fallback. Each reload takes a network
  // round trip plus RETRY_DELAY_MS, so two automatic attempts give a slow
  // deploy a few seconds to finish without ever spinning the tab.
  var MAX_AUTO_ATTEMPTS = 2;

  // Wait after the load settles before reloading, so a deployment that is
  // still copying files has a moment to complete. In the e2e harness this is
  // also the window in which a spec "finishes" the deploy.
  var RETRY_DELAY_MS = 600;

  var ATTEMPTS_KEY = 'mycorrhizal:skew:attempts';

  // The fallback message is rendered into the empty #root as plain DOM (no
  // innerHTML): the app bundle never loaded, so neither React nor i18n is
  // available. English only, like /_recovery.html -- this is a pre-app state
  // reached only when the deploy itself is broken.
  var FALLBACK_TITLE = 'Mycorrhizal CRM could not be loaded';
  var FALLBACK_BODY =
    'A required file was not found, which usually means a deployment was ' +
    'interrupted. Please try again.';
  var FALLBACK_RETRY = 'Try again';
  var FALLBACK_NOTE =
    'If this keeps happening, the server may still be starting up, or the ' +
    'deployment may need to be completed.';

  var sawBrokenAsset = false;
  var booted = false;
  var handled = false;
  var decisionScheduled = false;

  function rootElement() {
    return document.getElementById('root');
  }

  function assetUrlOf(target) {
    if (target?.nodeType !== 1) return null;
    var tag = target.tagName;
    if (tag !== 'SCRIPT' && tag !== 'LINK') return null;
    var url = target.src || target.href || '';
    if (!url) return null;
    var pathname;
    try {
      pathname = new URL(url, window.location.href).pathname;
    } catch {
      return null;
    }
    if (pathname.indexOf(ASSET_PREFIX) !== 0) return null;
    return pathname;
  }

  function readAttempts() {
    var raw = null;
    var parsed = 0;
    try {
      raw = window.sessionStorage.getItem(ATTEMPTS_KEY);
      parsed = raw ? Number(raw) : 0;
    } catch {
      // storage disabled -- count as zero attempts
    }
    return Number.isFinite(parsed) ? parsed : 0;
  }

  function writeAttempts(n) {
    try {
      window.sessionStorage.setItem(ATTEMPTS_KEY, String(n));
    } catch {
      // storage disabled -- best-effort only; each broken load then sees 0
    }
  }

  function clearAttempts() {
    try {
      window.sessionStorage.removeItem(ATTEMPTS_KEY);
    } catch {
      // ignore
    }
  }

  // A load error on a hashed app asset is the skew signature. Ignored once the
  // app has booted (a resource failure after boot is a different, per-surface
  // problem) or once this document has already acted.
  function onResourceError(event) {
    if (booted || handled) return;
    if (!assetUrlOf(event.target)) return;
    if (appIsBooted()) {
      // The app is rendering; whatever this asset was, the page is healthy.
      booted = true;
      clearAttempts();
      return;
    }
    sawBrokenAsset = true;
    scheduleDecision();
  }

  function onUnhandledRejection(event) {
    if (booted || handled) return;
    if (appIsBooted()) {
      booted = true;
      clearAttempts();
      return;
    }
    var reason = event?.reason;
    var message = reason?.message || String(reason || '');
    // A dynamic-import failure for a hashed chunk (defense in depth -- the app
    // currently only statically imports, but future lazy routes would surface
    // here). A rejection without any asset signal is not ours to act on.
    if (message.indexOf(ASSET_PREFIX) === -1 && message.indexOf('imported module') === -1) {
      return;
    }
    sawBrokenAsset = true;
    scheduleDecision();
  }

  function appIsBooted() {
    var root = rootElement();
    return !!root && root.childElementCount > 0;
  }

  // Observe #root so a successful boot clears the consecutive-failure counter
  // and arms nothing further on this document.
  function watchForBoot() {
    var root = rootElement();
    if (!root) return;
    if (root.childElementCount > 0) {
      booted = true;
      clearAttempts();
      return;
    }
    var observer;
    try {
      observer = new MutationObserver(() => {
        if (appIsBooted()) {
          booted = true;
          clearAttempts();
          observer.disconnect();
        }
      });
      observer.observe(root, { childList: true, subtree: true });
    } catch {
      // no MutationObserver (ancient browser) -- fall back to load-time checks
    }
  }

  function scheduleDecision() {
    if (decisionScheduled) return;
    decisionScheduled = true;
    // Act once the document has fully loaded: a module fetch failure resolves
    // before window load on every engine we support (verified empirically for
    // the two-build harness on Chromium and Firefox), and window load fires
    // even when a module failed -- so load is a reliable "the module graph has
    // settled" signal. If the error arrived after load already fired, decide
    // immediately.
    var settle = () => {
      if (handled) return;
      if (appIsBooted()) {
        booted = true;
        clearAttempts();
        return;
      }
      window.setTimeout(decide, RETRY_DELAY_MS);
    };
    if (document.readyState === 'complete') {
      settle();
      return;
    }
    window.addEventListener('load', settle);
  }

  function decide() {
    if (handled) return;
    if (!sawBrokenAsset || appIsBooted()) {
      handled = true;
      return;
    }
    handled = true;

    var attempts = readAttempts();
    if (attempts >= MAX_AUTO_ATTEMPTS) {
      renderFallback();
      return;
    }
    writeAttempts(attempts + 1);

    // Best-effort: if a service worker is present, ask it to look for an
    // update before the reload, so a reload that lands after the deploy
    // completes is served by the new worker rather than a stale cache.
    if (window.navigator && 'serviceWorker' in window.navigator) {
      window.navigator.serviceWorker
        .getRegistration()
        .then((registration) => {
          if (registration) return registration.update();
          return undefined;
        })
        .catch(() => {
          // offline / no worker -- the plain reload below still retries
        });
    }

    window.location.reload();
  }

  function renderFallback() {
    var root = rootElement();
    var host = root || document.body;
    if (!host) return;

    while (host.firstChild) host.removeChild(host.firstChild);

    var style = document.createElement('style');
    style.textContent =
      'body{margin:0;font-family:system-ui,-apple-system,"Segoe UI",Roboto,sans-serif;' +
      'background:#f5f6f8;color:#1c1e21}' +
      '.skew-card{max-width:30rem;margin:12vh auto 0;padding:2rem;background:#fff;' +
      'border:1px solid #d5d8dc;border-radius:8px}' +
      'h1{font-size:1.35rem;margin:0 0 .75rem}' +
      'p{line-height:1.5;margin:.5rem 0}' +
      'button{font:inherit;padding:.5rem 1rem;border-radius:6px;border:1px solid #1466b8;' +
      'background:#1466b8;color:#fff;cursor:pointer}' +
      '.note{color:#5f6368;font-size:.9rem}';
    document.head.appendChild(style);

    var card = document.createElement('div');
    card.className = 'skew-card';

    var title = document.createElement('h1');
    title.textContent = FALLBACK_TITLE;
    card.appendChild(title);

    var body = document.createElement('p');
    body.textContent = FALLBACK_BODY;
    card.appendChild(body);

    var retry = document.createElement('button');
    retry.type = 'button';
    retry.textContent = FALLBACK_RETRY;
    retry.addEventListener('click', () => {
      // A manual retry is a fresh start: clear the counter so a recovered
      // deploy is not kept in the fallback state.
      clearAttempts();
      handled = false;
      window.location.reload();
    });
    card.appendChild(retry);

    var note = document.createElement('p');
    note.className = 'note';
    note.textContent = FALLBACK_NOTE;
    card.appendChild(note);

    host.appendChild(card);
  }

  window.addEventListener('error', onResourceError, true);
  window.addEventListener('unhandledrejection', onUnhandledRejection);
  window.addEventListener('load', watchForBoot);
  document.addEventListener('DOMContentLoaded', watchForBoot);
})();
