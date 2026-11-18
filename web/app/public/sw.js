// The service worker that makes the dashboard installable. It caches nothing
// but the offline page: the page and its data always come from the server, so
// an installed app can never show a stale dashboard. When the server cannot
// be reached, a navigation gets the offline page instead of the browser's
// error.
//
// It also shows the desk's push messages (internal/cli/push.go): "show"
// raises a notification tagged with the item's display id, "close" takes it
// back once the item no longer needs the user, and a tap opens the item.
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

self.addEventListener('push', (e) => {
  let m = {};
  try {
    m = e.data ? e.data.json() : {};
  } catch {
    /* not JSON: show a plain notice */
  }
  if (m.type === 'close') {
    e.waitUntil(self.registration.getNotifications({ tag: m.tag }).then((ns) => ns.forEach((n) => n.close())));
    return;
  }
  e.waitUntil(
    self.registration.showNotification(m.title || 'Agent desk', {
      body: m.body || '',
      tag: m.tag || 'agent-desk',
      renotify: true,
      requireInteraction: !!m.sticky,
      icon: new URL('icon-192.png', self.registration.scope).href,
      data: { url: new URL(m.url || '', self.registration.scope).href },
    }),
  );
});

self.addEventListener('notificationclick', (e) => {
  e.notification.close();
  const url = e.notification.data?.url || self.registration.scope;
  e.waitUntil(
    self.clients.matchAll({ type: 'window', includeUncontrolled: true }).then((cs) => {
      const c = cs.find((x) => x.url.startsWith(self.registration.scope));
      if (c) return c.navigate(url).then((x) => (x || c).focus()).catch(() => c.focus());
      return self.clients.openWindow(url);
    }),
  );
});
