// Expo fixture (#2859) — proves Expo's full Data Flow/state_management surface
// (was partial). Expo apps are React Native apps; state management is handled
// by the same extractor (#513 setters + #2590 zustand store actions). Proves:
//   - state_management   → useState setter, useReducer dispatch, zustand action
//   - state_setter_emission → subtype="state_setter"
//   - context_extraction → createContext()
//   - hoc_wrapper_recognition → memo()
import React, { createContext, useState, useReducer, memo } from 'react';
import { View } from 'react-native';
import { create } from 'zustand';

// context_extraction
export const ThemeContext = createContext('light');

// zustand store
export const useSessionStore = create((set) => ({
  token: null,
  setToken: (t) => set({ token: t }),
  logout: () => set({ token: null }),
}));

function prefsReducer(state, action) {
  return state;
}

function ProfileScreen() {
  // state_management + state_setter_emission
  const [name, setName] = useState('');
  const [theme, setTheme] = useState('light');
  const [prefs, dispatchPrefs] = useReducer(prefsReducer, {});

  const logout = useSessionStore((s) => s.logout);

  // branch_conditions
  function save() {
    if (theme === 'dark') {
      setName(name);
    }
    dispatchPrefs({ type: 'save' });
    logout();
  }

  return <View onLayout={save} />;
}

export default memo(ProfileScreen);
