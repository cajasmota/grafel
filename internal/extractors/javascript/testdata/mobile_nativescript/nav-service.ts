// NativeScript navigation + native-bridge + platform-branch fixture (#2860).
//
// Exercises:
//   - Frame.topmost().navigate({ moduleName: 'home-page' }) → screens + edge
//   - registerElement('CardView', () => CardView)           → navigators
//   - import { isIOS, Device } from '@nativescript/core'     → native_modules + platform_branches
//   - handleOpenURL(handler)                                 → deep_link
import { Frame, isIOS, Device } from '@nativescript/core';
import { registerElement } from '@nativescript/core/ui/builder';
import { handleOpenURL } from 'nativescript-urlhandler';

registerElement('CardView', () => CardView);

export function goHome() {
  Frame.topmost().navigate({ moduleName: 'home-page' });
}

export function goProfile() {
  Frame.topmost().navigate({ moduleName: 'profile-page' });
}

export function bootDeepLinks(onRoute: (path: string) => void) {
  handleOpenURL((appURL) => {
    onRoute(appURL.path);
  });
}

export function platformTag(): string {
  if (isIOS) {
    return 'ios-' + Device.os;
  }
  return 'android';
}
