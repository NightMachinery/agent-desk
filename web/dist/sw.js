// The service worker that makes the dashboard installable. It caches nothing
// but the offline page: the page and its data always come from the server, so
// an installed app can never show a stale dashboard. When the server cannot
// be reached, a navigation gets the offline page instead of the browser's
// error.
const CACHE = 'agent-desk-offline-1';
const OFFLINE = new URL('offline.html', self.registration.scope).href;

self.addEventListener('install', (e) => {
  e.waitUntil(caches.open(CACHE).then((c) => c.add(new Request(OFFLINE, { cache: 'reload' }))));
  self.skipWaiting();
});

self.addEventListener('activate', (e) => {
  e.waitUntil(
    caches
      .keys()
      .then((keys) => Promise.all(keys.filter((k) => k.startsWith('agent-desk-') && k !== CACHE).map((k) => caches.delete(k))))
      .then(() => self.clients.claim()),
  );
});

self.addEventListener('fetch', (e) => {
  if (e.request.mode !== 'navigate') return;
  e.respondWith(fetch(e.request).catch(() => caches.match(OFFLINE)));
});
