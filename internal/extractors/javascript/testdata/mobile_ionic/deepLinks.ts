// Ionic / Capacitor deep-link fixture (#2860).
//
// Capacitor surfaces deep links via App.addListener('appUrlOpen', cb); the
// handler parses the incoming URL and routes. This is the genuine Ionic
// deep-link entry point (vs RN's Linking.createURL).
import { App } from '@capacitor/app';

export function registerDeepLinks(navigate: (path: string) => void) {
  App.addListener('appUrlOpen', (event) => {
    const slug = event.url.split('.app').pop();
    if (slug) {
      navigate(slug);
    }
  });
}
