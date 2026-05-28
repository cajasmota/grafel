// Mobile (React Native / Expo / Ionic / NativeScript) fixture — proves
// import-resolution quality for the jsts substrate sniffer (issue #2850).
// Covers all four mobile frameworks in one hand-written fixture; no node_modules.
//
// React Native import shape:
import { View, Text, Pressable } from 'react-native';
import { useNavigation } from '@react-navigation/native';
// Expo import shape:
import { Camera } from 'expo-camera';
import * as FileSystem from 'expo-file-system';
// Ionic import shape (Ionic + Capacitor):
import { IonContent, IonHeader, IonPage } from '@ionic/react';
import { Filesystem, Directory } from '@capacitor/filesystem';
// NativeScript import shape:
import { Frame, Page } from '@nativescript/core';

const API_URL = 'https://api.example.com';
const SECRET = process.env.RN_API_KEY ?? 'dev-only';

// import.meta.env for Expo/Vite-based toolchains
const EXPO_API = import.meta.env.EXPO_PUBLIC_API_URL ?? 'https://api.example.com';

export default function App() {
  const navigation = useNavigation();
  return null;
}
