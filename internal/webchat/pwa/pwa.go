// Package pwa provides the PWA (Progressive Web App) surface for the
// built-in web chat: a manifest and a service worker that together let
// users install the AI Butler web UI to their phone/desktop home screen
// as a standalone app.
//
// Two handlers, mounted at the root of the HTTP mux:
//
//	GET /manifest.json — application/manifest+json
//	GET /sw.js         — application/javascript (with Service-Worker-Allowed: /)
//
// Plus /apple-touch-icon.png which iOS fetches automatically.
//
// The manifest and icons live in the static FS of the webchat adapter;
// this package only serves the dynamic header metadata and the service
// worker script (which is kept here so cache versioning can be bumped
// without touching the static tree).
package pwa

import (
	"net/http"
)

// manifestJSON is served at /manifest.json. Kept inline so the cache
// version string and icon paths stay co-located with the service worker.
const manifestJSON = `{
  "name": "AI Butler",
  "short_name": "Butler",
  "description": "Self-hosted personal AI agent with exceptional memory.",
  "start_url": "/",
  "scope": "/",
  "display": "standalone",
  "orientation": "any",
  "background_color": "#1a1a2e",
  "theme_color": "#16213e",
  "categories": ["productivity", "utilities"],
  "icons": [
    { "src": "/static/icon-192.png", "sizes": "192x192", "type": "image/png", "purpose": "any" },
    { "src": "/static/icon-512.png", "sizes": "512x512", "type": "image/png", "purpose": "any" },
    { "src": "/static/icon-maskable-512.png", "sizes": "512x512", "type": "image/png", "purpose": "maskable" }
  ]
}`

// serviceWorkerJS is served at /sw.js. Strategy:
//   - Precache a tiny static shell on install so the app opens offline.
//   - Runtime: cache-first for /static/*, network-first for everything else
//     (so the dashboard API / WebSocket never gets stuck on a stale cached
//     response). The /ws WebSocket endpoint is bypassed entirely since
//     service workers cannot intercept WebSocket upgrades anyway.
//   - Navigation fallback: if the network is unreachable, serve the
//     cached root so the shell still opens.
//
// Cache version is baked into CACHE_NAME so an `activate` event can
// clean out older versions on upgrade.
const serviceWorkerJS = `// AI Butler Service Worker
// Bump CACHE_NAME on breaking changes — the activate handler deletes older caches.
const CACHE_NAME = 'butler-cache-v2';
const PRECACHE = [
  '/',
  '/static/index.html',
  '/static/style.css',
  '/static/chat.js',
  '/static/icon-192.png',
  '/static/icon-512.png',
  '/manifest.json'
];

self.addEventListener('install', (event) => {
  event.waitUntil(
    caches.open(CACHE_NAME).then((cache) => cache.addAll(PRECACHE))
      .then(() => self.skipWaiting())
  );
});

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches.keys().then((names) => Promise.all(
      names.filter((name) => name !== CACHE_NAME).map((name) => caches.delete(name))
    )).then(() => self.clients.claim())
  );
});

self.addEventListener('fetch', (event) => {
  const req = event.request;
  if (req.method !== 'GET') return;

  const url = new URL(req.url);
  // Never intercept WebSocket or upload endpoints.
  if (url.pathname === '/ws' || url.pathname === '/upload') return;

  // Cache-first for static assets (they are content-hashed implicitly
  // by the cache version bump, and change infrequently).
  if (url.pathname.startsWith('/static/') || url.pathname === '/manifest.json') {
    event.respondWith(
      caches.match(req).then((cached) => cached || fetch(req).then((res) => {
        const clone = res.clone();
        caches.open(CACHE_NAME).then((cache) => cache.put(req, clone));
        return res;
      }))
    );
    return;
  }

  // Network-first for everything else (dashboard API, index HTML),
  // falling back to cache when offline.
  event.respondWith(
    fetch(req).then((res) => {
      if (res.ok) {
        const clone = res.clone();
        caches.open(CACHE_NAME).then((cache) => cache.put(req, clone));
      }
      return res;
    }).catch(() => caches.match(req).then((cached) => {
      if (cached) return cached;
      if (req.mode === 'navigate') return caches.match('/');
      return new Response('Offline', { status: 503, statusText: 'Offline' });
    }))
  );
});
`

// ManifestHandler returns an HTTP handler that serves the PWA manifest.json.
// Mount at /manifest.json on the webchat mux.
func ManifestHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/manifest+json")
		// Cache for an hour — the manifest is small and changes rarely,
		// but we don't want users stuck with a broken one across an update.
		w.Header().Set("Cache-Control", "public, max-age=3600")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(manifestJSON))
	})
}

// ServiceWorkerHandler returns an HTTP handler that serves the service worker JS.
// Mount at /sw.js on the webchat mux. MUST be served from the same origin and
// path scope as the pages it controls; the Service-Worker-Allowed header
// widens the scope to the whole site so the SW at /sw.js controls /.
func ServiceWorkerHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Header().Set("Service-Worker-Allowed", "/")
		// SW script itself should NOT be cached by HTTP caches — the browser
		// handles SW lifecycle separately.
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(serviceWorkerJS))
	})
}
