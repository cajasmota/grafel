// Ionic navigation + native-bridge + platform-branch fixture (#2860).
//
// Exercises:
//   - <IonReactRouter>/<IonRouterOutlet>  → navigators
//   - <Route path="/home" />              → screens + NAVIGATES_TO via=screen_config
//   - import ... from '@capacitor/geolocation' → native_modules
//   - Capacitor.getPlatform() / isPlatform('ios') → platform_branches
import React from 'react';
import { IonApp, IonRouterOutlet, isPlatform } from '@ionic/react';
import { IonReactRouter } from '@ionic/react-router';
import { Route } from 'react-router-dom';
import { Geolocation } from '@capacitor/geolocation';
import { Capacitor } from '@capacitor/core';

export async function currentPosition() {
  return Geolocation.getCurrentPosition();
}

export function platformLabel(): string {
  if (isPlatform('ios')) {
    return 'iOS';
  }
  if (Capacitor.getPlatform() === 'android') {
    return 'Android';
  }
  return 'Web';
}

export function AppRouter() {
  return (
    <IonApp>
      <IonReactRouter>
        <IonRouterOutlet>
          <Route path="/home" component={Home} exact />
          <Route path="/profile" component={Profile} />
          <Route path="/settings" component={Settings} />
        </IonRouterOutlet>
      </IonReactRouter>
    </IonApp>
  );
}
