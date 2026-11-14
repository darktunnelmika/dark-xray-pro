// Minimal service worker so the panel installs as an app. It never caches
// API answers: the panel always shows live data, and works only online.
self.addEventListener("install", () => self.skipWaiting());
self.addEventListener("activate", (e) => e.waitUntil(self.clients.claim()));
self.addEventListener("fetch", () => {});
