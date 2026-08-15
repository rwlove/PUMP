// PUMP — main-app service worker.
//
// Purpose: make PUMP an installable PWA. Brave/Chrome only offer the real
// "Install app" flow (standalone window + the manifest icon, not a monogram
// bookmark) when a service worker with a fetch handler controls the manifest
// start_url. This file is served from /sw.js (NOT /fs/public/…) so its default
// scope is the whole app ("/") — a worker under /fs/ could only ever control
// /fs/, which is why it must live at the root.
//
// Strategy is deliberately conservative — PUMP is a live tracker and stale
// content is a footgun:
//   - Navigations (HTML): network-first. Always fresh when online; fall back
//     to the last cached copy only when the network is unreachable.
//   - Everything else (API, SSE, static assets): pure pass-through, no caching.
//   - /wall/ is left entirely alone — the kiosk view owns its own worker.

const CACHE_NAME = "pump-app-v1";
const OFFLINE_FALLBACK = "/";

self.addEventListener("install", (event) => {
  event.waitUntil(
    caches.open(CACHE_NAME)
      .then((c) => c.add(OFFLINE_FALLBACK))
      .catch(() => {})
      .then(() => self.skipWaiting())
  );
});

self.addEventListener("activate", (event) => {
  event.waitUntil(
    caches.keys()
      .then((keys) => Promise.all(keys.filter((k) => k !== CACHE_NAME).map((k) => caches.delete(k))))
      .then(() => self.clients.claim())
  );
});

self.addEventListener("fetch", (event) => {
  const req = event.request;
  if (req.method !== "GET") return;
  const url = new URL(req.url);

  // The kiosk view owns its own worker/caching — never intercept it.
  if (url.pathname.startsWith("/wall/")) return;

  // Navigations: network-first, caching the result as an offline fallback.
  if (req.mode === "navigate") {
    event.respondWith(
      fetch(req)
        .then((resp) => {
          if (resp && resp.ok) {
            const copy = resp.clone();
            caches.open(CACHE_NAME).then((c) => c.put(req, copy)).catch(() => {});
          }
          return resp;
        })
        .catch(() => caches.match(req).then((hit) => hit || caches.match(OFFLINE_FALLBACK)))
    );
    return;
  }

  // Everything else: pass through to the network untouched.
});
