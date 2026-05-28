// Expo platform-variant + Platform.OS branch fixture (#2860).
//
// The `.ios.tsx` filename itself is a platform branch (file:ios); the in-body
// Platform.OS comparison is a second platform branch.
import { Platform } from 'react-native';
import Constants from 'expo-constants';

export function statusBarHeight(): number {
  if (Platform.OS === 'ios') {
    return Constants.statusBarHeight;
  }
  return 0;
}
