# React Native convention

Required reading: `_graph-searchability.md`.

Applies to React Native apps (Expo and bare workflow). For React on the web, see `react.md`.

## Public surface

1. **Screens** — every screen registered with the navigator (React Navigation, Expo Router).
2. **Deep links / URL schemes** — `Linking` config, `expo-linking`, intent filters.
3. **Push-notification handlers** — registered with Expo Notifications, FCM, or APNs.
4. **Native modules** — anything bridged from `ios/` or `android/`, exposed via `NativeModules.*`.
5. **Background tasks** — `expo-background-fetch`, `react-native-background-fetch`, headless JS tasks.

## Module shape

Feature folders, similar to React on web, plus a hard split between platform-shared and platform-specific files. Document `*.ios.tsx` / `*.android.tsx` divergence explicitly.

## Entry points (Pass 3)

- `index.js` — registers the root component with `AppRegistry`.
- The navigator root (e.g., `app/_layout.tsx` for Expo Router, `App.tsx` for classic stack).
- `app.json` / `app.config.ts` — runtime config for Expo.
- Native entry points: `ios/<App>/AppDelegate.m[m]`, `android/app/src/main/java/.../MainActivity.kt` — only describe these if they were customized.

## Dynamic edges (Pass 4)

- **Deep links** route into screens by URL pattern. Encode each pattern → screen in `flows.md`:
  ```markdown
  ## How `myapp://order/:id` opens `OrderDetailScreen`
  ```
- **Push notifications** — payload shape → handler is a runtime contract. Capture both in a heading.
- **Native module calls** — JS calls a native bridge method; the implementation is in another file in another language. Always describe both ends.
- **Persistence layers** — AsyncStorage / MMKV keys are global strings; document key → owner.

## Deployment signals (Pass 5)

- **EAS Build profiles** — `eas.json`. Document each profile.
- **App Store / Play Store metadata** — usually in `app.json` or `eas.json`.
- **OTA updates** — `expo-updates` channel/runtime-version pairing. Document when a bundle counts as a hotfix vs a store release.
- **Build numbers** — note the source of truth.

## Manifest files (Pass 5)

`package.json` (JS), `Podfile`/`Podfile.lock` (iOS), `android/app/build.gradle` and `android/build.gradle` (Android). List native deps separately from JS deps.

## Cross-cutting pitfalls

- **Permissions** — iOS `Info.plist` and Android `AndroidManifest.xml` must declare every permission the app uses. Mismatch with runtime requests is a top-3 source of bugs.
- **Networking on cellular vs Wi-Fi** — retry/backoff policy belongs in `cross-cutting/errors.md`.
- **Background execution** — iOS and Android have different lifecycles for background work. Document the lifecycle assumptions.

## Cross-repo signals

Mobile typically connects to: a backend HTTP API, a real-time channel, a push-notification service, and analytics. Each is a candidate cross-repo edge. The backend repo is almost always present in the same grafel group; the others are usually external.
