// Service Worker: App-Hülle offline, Web-Push. Daten kommen aus IndexedDB der Seite, nicht aus diesem Cache.
const CACHE = 'sprachnotizen-__V__';
const HUELLE = ['/', '/app.js?v=__V__', '/app.css?v=__V__', '/theme.js?v=__V__', '/manifest.webmanifest', '/icon.svg', '/icon-192.png', '/icon-512.png'];

self.addEventListener('install', (e) => {
  e.waitUntil(caches.open(CACHE).then((c) => c.addAll(HUELLE)).then(() => self.skipWaiting()));
});

self.addEventListener('activate', (e) => {
  e.waitUntil(caches.keys().then((ks) => Promise.all(ks.filter((k) => k !== CACHE).map((k) => caches.delete(k)))).then(() => self.clients.claim()));
});

self.addEventListener('fetch', (e) => {
  const url = new URL(e.request.url);
  if (e.request.method !== 'GET' || url.origin !== location.origin || url.pathname.startsWith('/api/') || url.pathname.startsWith('/oauth/') || url.pathname.startsWith('/.well-known/') || url.pathname === '/mcp' || url.pathname === '/hook' || url.pathname === '/sw') return;
  // Erst das Netz (immer aktueller Stand), ohne Netz die gespeicherte Hülle.
  const key = e.request.mode === 'navigate' ? '/' : url.pathname + url.search;
  e.respondWith(
    fetch(e.request).then((antwort) => {
      if (antwort.ok && HUELLE.includes(key)) {
        const kopie = antwort.clone();
        caches.open(CACHE).then((c) => c.put(key, kopie));
      }
      return antwort;
    }).catch(() => caches.match(key).then((treffer) => treffer || Response.error())),
  );
});

self.addEventListener('push', (e) => {
  let d = {};
  try { d = e.data.json(); } catch (f) { /* leer */ }
  e.waitUntil(self.registration.showNotification(d.titel || 'Sprachnotizen', {
    body: d.text || '', icon: '/icon-192.png', badge: '/icon-192.png', data: { pfad: d.pfad || '/' }, tag: d.pfad || undefined,
  }));
});

self.addEventListener('notificationclick', (e) => {
  e.notification.close();
  const pfad = e.notification.data?.pfad || '/';
  e.waitUntil(self.clients.matchAll({ type: 'window', includeUncontrolled: true }).then((fenster) => {
    for (const f of fenster) {
      if ('focus' in f) { f.navigate(pfad); return f.focus(); }
    }
    return self.clients.openWindow(pfad);
  }));
});
