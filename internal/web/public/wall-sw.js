// PUMP wall view — service worker.
//
// Strategy:
//   - Pre-cache the shell (HTML/CSS/JS) on install so the kiosk can
//     boot the UI even if PUMP is briefly unreachable. The shell will
//     show "connecting…" until SSE re-establishes.
//   - Network-first for everything under /api/ — never serve stale
//     data, just fall back to cache for read-only GET if the network
//     is down.
//   - Cache-first for /fs/ (static JS/CSS/icons) and /clips/ (videos).
//
// Versioning: bump CACHE_NAME when the shell changes meaningfully so
// older caches get evicted on next activation.

const CACHE_NAME = "pump-wall-v2";
const SHELL = [
  "/wall/",
  "/fs/public/css/wall.css",
  "/fs/public/js/wall.js",
  "/fs/public/favicon.svg",
];

self.addEventListener("install", (event) => {
  event.waitUntil(
    caches.open(CACHE_NAME).then((c) => c.addAll(SHELL)).then(() => self.skipWaiting())
  );
});

self.addEventListener("activate", (event) => {
  event.waitUntil(
    caches.keys().then((keys) =>
      Promise.all(keys.filter((k) => k !== CACHE_NAME).map((k) => caches.delete(k)))
    ).then(() => self.clients.claim())
  );
});

self.addEventListener("fetch", (event) => {
  const req = event.request;
  if (req.method !== "GET") return;
  const url = new URL(req.url);

  // SSE streams must always go to network — caching them would be a
  // disaster (think: replaying yesterday's events forever).
  if (url.pathname.endsWith("/stream")) return;

  // Live camera snapshots: pure passthrough to the network. Each poll
  // carries a different ?t= cache-buster, so caching every response
  // would grow the cache unboundedly with effectively-duplicate images.
  if (url.pathname.includes("/cameras/") && url.pathname.endsWith("/snapshot")) return;

  // /api/: network-first, fall back to cache only for safe reads.
  if (url.pathname.startsWith("/api/")) {
    event.respondWith(
      fetch(req).then((resp) => {
        if (resp.ok && req.method === "GET") {
          const copy = resp.clone();
          caches.open(CACHE_NAME).then((c) => c.put(req, copy)).catch(() => {});
        }
        return resp;
      }).catch(() => caches.match(req))
    );
    return;
  }

  // /fs/, /clips/, /wall/: cache-first, refresh in background.
  event.respondWith(
    caches.match(req).then((hit) => {
      const fetchPromise = fetch(req).then((resp) => {
        if (resp && resp.ok) {
          const copy = resp.clone();
          caches.open(CACHE_NAME).then((c) => c.put(req, copy)).catch(() => {});
        }
        return resp;
      }).catch(() => hit);
      return hit || fetchPromise;
    })
  );
});
