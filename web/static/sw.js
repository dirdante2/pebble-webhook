// Service Worker: App-Hülle offline, Web-Push. Daten kommen aus IndexedDB der Seite, nicht aus diesem Cache.
// Die Hülle gehört zu genau einer App-Version (__V__): sie kommt immer aus dem Cache und wird nur mit einem
// neuen Service Worker (= neue Version) einmal neu geladen. Danach lädt die Seite beim nächsten Wechsel neu.
const CACHE = 'sprachnotizen-__V__';
const HUELLE = ['/', '/app.js?v=__V__', '/app.css?v=__V__', '/theme.js?v=__V__', '/manifest.webmanifest', '/icon.svg', '/icon-192.png', '/icon-512.png'];

self.addEventListener('install', (e) => {
  // cache: 'no-cache' – nach ETag nachfragen, damit keine Hülle einer älteren Version im Browser-Speicher übernommen wird.
  e.waitUntil(caches.open(CACHE).then((c) => c.addAll(HUELLE.map((u) => new Request(u, { cache: 'no-cache' })))).then(() => self.skipWaiting()));
});

self.addEventListener('activate', (e) => {
  e.waitUntil(caches.keys().then((ks) => Promise.all(ks.filter((k) => k !== CACHE).map((k) => caches.delete(k)))).then(() => self.clients.claim()));
});

self.addEventListener('fetch', (e) => {
  const url = new URL(e.request.url);
  if (e.request.method !== 'GET' || url.origin !== location.origin || url.pathname.startsWith('/api/') || url.pathname.startsWith('/oauth/') || url.pathname.startsWith('/.well-known/') || url.pathname === '/mcp' || url.pathname === '/hook' || url.pathname === '/sw') return;
  const seite = e.request.mode === 'navigate';
  const key = seite ? '/' : url.pathname + url.search;
  if (!seite && !HUELLE.includes(key)) return;
  e.respondWith(caches.open(CACHE).then(async (c) => {
    const treffer = await c.match(key);
    const netz = fetch(seite ? '/' : e.request).then((antwort) => {
      if (antwort.ok) c.put(key, antwort.clone());
      return antwort;
    });
    if (!treffer) return netz;
    // Seite: sofort aus dem Cache, im Hintergrund nachfragen (meist 304, z. B. neue Sprache).
    if (seite) e.waitUntil(netz.catch(() => {}));
    return treffer;
  }).catch(() => Response.error()));
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
