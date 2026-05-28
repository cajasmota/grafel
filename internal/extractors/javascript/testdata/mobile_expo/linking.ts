// Expo deep-link + native-bridge fixture (#2860).
//
// Exercises:
//   - Linking.createURL('/redirect')        → deep_link_prefixes + NAVIGATES_TO via=deep_link
//   - const linking = { prefixes, config: { screens } } → deep_link_screens + edges
//   - import ... from 'expo-secure-store'    → native_modules
//   - requireNativeModule('ExpoDevice')      → native_modules
import * as Linking from 'expo-linking';
import * as SecureStore from 'expo-secure-store';
import { requireNativeModule } from 'expo-modules-core';

const ExpoDevice = requireNativeModule('ExpoDevice');

export const redirectUri = Linking.createURL('/redirect');

export const linking = {
  prefixes: ['myapp://', 'https://app.example.com'],
  config: {
    screens: {
      Home: '',
      Profile: 'user/:id',
      Settings: 'settings',
    },
  },
};

export async function saveSession(token: string) {
  await SecureStore.setItemAsync('session', token);
  return ExpoDevice.modelName;
}
