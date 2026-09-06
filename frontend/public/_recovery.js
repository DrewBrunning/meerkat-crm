// Issue #476 (WEB-02) escape hatch, served from /_recovery.js.
//
// A service-worker failure is the one client bug that survives the remedy: a
// bad worker keeps serving its broken cache after the deploy that fixed it,
// and a normal refresh never reaches the network. This script is the
// deliberate way out. It runs on the static /_recovery.html page, which the
// app's service worker is written never to intercept (the navigation handler
// excludes /_* paths -- see src/service-worker.ts), so it is reachable even
// while a broken worker is in control.
//
// It clears every CacheStorage bucket and unregisters every service worker,
// then reloads from the network. No application code needs to be running for
// it to work: this file and _recovery.html ship as plain static assets and are
// excluded from the workbox precache manifest (vite.config.ts globIgnores), so
// a cached copy of a broken build can never shadow them.

(function () {
  'use strict';

  function setStatus(text) {
    const el = document.getElementById('status');
    if (el) {
      el.textContent = text;
    }
  }

  async function recover() {
    let cleared = 0;
    if ('caches' in window) {
      try {
        let keys = await caches.keys();
        for (let i = 0; i < keys.length; i++) {
          await caches.delete(keys[i]);
          cleared += 1;
        }
      } catch (err) {
        // Clearing is best-effort; unregister below is the part that must not
        // be skipped, so swallow and continue.
        console.warn('recovery: cache clearing failed', err);
      }
    }

    if ('serviceWorker' in navigator) {
      try {
        let registrations = await navigator.serviceWorker.getRegistrations();
        await Promise.all(
          registrations.map(function (registration) {
            return registration.unregister();
          }),
        );
      } catch (err) {
        console.warn('recovery: service worker unregister failed', err);
      }
    }

    setStatus('Cleared ' + cleared + ' cached bucket(s). Reloading\u2026');

    // The reload must not be held hostage by whatever worker was in control:
    // after unregister() the navigation falls through to the network, which is
    // the entire point. A short delay lets the status line render.
    setTimeout(function () {
      window.location.replace('/');
    }, 150);
  }

  recover();
})();
