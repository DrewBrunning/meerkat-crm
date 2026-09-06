// Issue #476 (WEB-02): the "broken deploy" fixture worker, served by the
// sw-upgrade harness in place of the real service-worker.js.
//
// It models the class of failure the escape hatch exists for: a deploy whose
// service worker is bad in a way that keeps serving broken content no matter
// how often the user refreshes. This worker installs, activates and claims
// like a healthy one, but answers every in-scope navigation with a canned
// broken document -- so the app is unusable and a plain reload never reaches
// the server.
//
// Crucially it honours the same contract as the real worker
// (frontend/src/service-worker.ts): navigations under /_ are left to the
// network. That is what keeps /_recovery.html reachable while this worker is
// in control -- and it is the property the escape-hatch test pins: the
// recovery page must stay reachable from behind a broken worker.

self.addEventListener('message', (event) => {
  // Only ever honour control messages from a client of this origin. The real
  // worker makes the same check (src/service-worker.ts); mirroring it here
  // keeps the fixture a faithful stand-in for the product.
  if (event.origin !== self.location.origin) {
    return;
  }
  if (event.data && event.data.type === 'SKIP_WAITING') {
    self.skipWaiting();
  }
});

self.addEventListener('activate', (event) => {
  event.waitUntil(self.clients.claim());
});

self.addEventListener('fetch', (event) => {
  const url = new URL(event.request.url);
  if (url.origin !== self.location.origin) {
    return;
  }
  if (event.request.mode !== 'navigate') {
    return;
  }
  if (url.pathname.startsWith('/_')) {
    return;
  }
  event.respondWith(
    new Response(
      '<!doctype html><html><head><meta charset="utf-8"><title>broken deploy</title></head>' +
        '<body><h1>Service worker broken</h1><p id="sw-broken">sw-broken-deploy</p>' +
        '<p>This page is served by a deliberately broken service worker fixture.</p></body></html>',
      { status: 200, headers: { 'Content-Type': 'text/html; charset=utf-8' } },
    ),
  );
});
