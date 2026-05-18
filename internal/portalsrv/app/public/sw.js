// Service worker has been removed.
// This script unregisters itself and clears all caches so browsers that
// previously installed the old service worker get cleaned up automatically.
self.addEventListener('install', () => self.skipWaiting());
self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches.keys()
      .then((keys) => Promise.all(keys.map((k) => caches.delete(k))))
      .then(() => self.registration.unregister())
      .then(() => self.clients.matchAll({ includeUncontrolled: true }))
      .then((clients) => clients.forEach((c) => c.navigate && c.navigate(c.url)))
  );
});
