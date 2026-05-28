// React Native navigation + native-bridge + platform-branch fixture (#2860).
//
// Exercises:
//   - createNativeStackNavigator()  → navigators
//   - <Stack.Screen name="Home" />  → screens + NAVIGATES_TO via=screen_config
//   - NativeModules.BiometricAuth   → native_modules
//   - import ... from 'react-native-keychain' → native_modules
//   - Platform.OS === 'ios' / Platform.select  → platform_branches
import React from 'react';
import { NativeModules, Platform } from 'react-native';
import { createNativeStackNavigator } from '@react-navigation/native-stack';
import * as Keychain from 'react-native-keychain';

const { BiometricAuth } = NativeModules;

const Stack = createNativeStackNavigator();

export function persistToken(token: string) {
  return Keychain.setGenericPassword('user', token);
}

export function biometricLabel(): string {
  if (Platform.OS === 'ios') {
    return 'Face ID';
  }
  return Platform.select({ android: 'Fingerprint', default: 'Biometrics' });
}

export async function authenticate() {
  return BiometricAuth.prompt('Confirm identity');
}

export function AppNavigator() {
  return (
    <Stack.Navigator>
      <Stack.Screen name="Home" component={Home} />
      <Stack.Screen name="Profile" component={Profile} />
      <Stack.Screen name="Settings" component={Settings} />
    </Stack.Navigator>
  );
}
