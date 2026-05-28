// Real-world-shaped React Native root navigator (#2860).
//
// Hand-written, dependency-manifest-free fixture modelled on a typical
// React Navigation + native-bridge + platform-branch app entry file. Used by
// the issue-#2860 real-data verification test to confirm the mobile signals
// (navigators / screens / deep_link / native_modules / platform_branches) fire
// on real-shaped source rather than a toy snippet.
import React, { useEffect } from 'react';
import { NativeModules, Platform, NativeEventEmitter } from 'react-native';
import { NavigationContainer } from '@react-navigation/native';
import { createNativeStackNavigator } from '@react-navigation/native-stack';
import { createBottomTabNavigator } from '@react-navigation/bottom-tabs';
import * as Linking from 'expo-linking';
import messaging from '@react-native-firebase/messaging';

import HomeScreen from './screens/HomeScreen';
import FeedScreen from './screens/FeedScreen';
import ProfileScreen from './screens/ProfileScreen';
import SettingsScreen from './screens/SettingsScreen';

const { HapticFeedback } = NativeModules;
const pushEmitter = new NativeEventEmitter(messaging);

const RootStack = createNativeStackNavigator();
const Tabs = createBottomTabNavigator();

export const linking = {
  prefixes: [Linking.createURL('/'), 'https://app.example.com', 'example://'],
  config: {
    screens: {
      Home: 'home',
      Feed: 'feed/:category',
      Profile: 'user/:id',
      Settings: 'settings',
    },
  },
};

function impactStyle(): string {
  if (Platform.OS === 'ios') {
    return 'impactMedium';
  }
  return Platform.select({ android: 'effectClick', default: 'impactLight' });
}

export function triggerHaptic() {
  HapticFeedback.trigger(impactStyle());
}

function MainTabs() {
  return (
    <Tabs.Navigator>
      <Tabs.Screen name="Home" component={HomeScreen} />
      <Tabs.Screen name="Feed" component={FeedScreen} />
      <Tabs.Screen name="Profile" component={ProfileScreen} />
    </Tabs.Navigator>
  );
}

export default function RootNavigator() {
  useEffect(() => {
    const sub = pushEmitter.addListener('notificationOpened', triggerHaptic);
    return () => sub.remove();
  }, []);

  return (
    <NavigationContainer linking={linking}>
      <RootStack.Navigator>
        <RootStack.Screen name="Main" component={MainTabs} />
        <RootStack.Screen name="Settings" component={SettingsScreen} />
      </RootStack.Navigator>
    </NavigationContainer>
  );
}
