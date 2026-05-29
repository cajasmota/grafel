// Mobile substrate proving fixture (#3059).
//
// Covers React Native / Expo / Ionic / NativeScript substrate cells.
// Proves that the framework-blind jsts substrate sniffers fire on
// mobile JS/TS source code:
//
//   - http_effect           — fetchProfile calls fetch()
//   - fs_effect             — readCache / writeCacheEntry call fs.readFile / fs.writeFile
//                             (React Native / Expo can bundle a polyfilled fs via
//                             expo-file-system mapped into the Node fs shim)
//   - mutation_effect       — DataStore.setUser assigns this._user
//   - taint_source_detection — req.body.userId in serverRoute
//   - taint_sink_detection  — dangerouslySetInnerHTML XSS sink in renderHtml
//   - sanitizer_recognition — DOMPurify.sanitize in renderHtml
//   - vulnerability_finding — source→sink in renderHtml (proves input pair)
//   - def_use_chain_extraction — const endpoint / result / parsed in fetchProfile
//   - pure_function_tagging — formatLabel has no side effects
//   - template_pattern_catalog — t("home.title"), console.error, SELECT literal
//   - request_shape_extraction — fetch body shape in postPayment
//   - response_shape_extraction — res.json({...}) in serverRoute
//   - import_resolution_quality — cross-file imports (already proven in App.tsx)
//
// db_effect is NOT expected on mobile (N/A): React Native / Expo / Ionic /
// NativeScript apps do not invoke Node.js ORM primitives like .findOne() /
// .create() — they call remote HTTP APIs instead.

import React from 'react';
import { View } from 'react-native';
import { useNavigation } from '@react-navigation/native';
import * as FileSystem from 'expo-file-system';
import { Filesystem } from '@capacitor/filesystem';
import { Frame } from '@nativescript/core';
import { formatUtil } from './utils';
import { cyclic_dep } from './cyclic_mobile';

// ── pure helper (no side effects) ────────────────────────────────────────────
// Proves pure_function_tagging: formatLabel has no db/http/fs/mutation effects;
// the pure-function pass should tag it pure.
export function formatLabel(name: string, count: number): string {
  return `${name} (${count})`;
}

// ── http_effect ───────────────────────────────────────────────────────────────
// Proves http_effect: fetchProfile calls the Fetch API.
export async function fetchProfile(userId: string) {
  const endpoint = 'https://api.example.com/profile';
  const result = await fetch(`${endpoint}/${userId}`);
  const parsed = await result.json();
  return parsed;
}

// ── fs_effect ─────────────────────────────────────────────────────────────────
// Proves fs_effect: React Native / Expo can use a Node fs shim; the substrate
// sniffer recognises the same fs.readFile / fs.writeFile primitives.
export async function readCache(path: string) {
  return await fs.readFile(path, 'utf8');
}

export async function writeCacheEntry(path: string, data: string) {
  await fs.writeFile(path, data);
}

// ── mutation_effect ───────────────────────────────────────────────────────────
// Proves mutation_effect: setUser assigns this._user.
class DataStore {
  _user: object | null = null;

  setUser(data: object) {
    this._user = data;
  }
}

// ── taint source + sink + sanitizer + vulnerability_finding ──────────────────
// Proves taint_source_detection: req.body.userId is matched by jstsSourceReqRe.
// Proves taint_sink_detection: dangerouslySetInnerHTML matches jstsSinkXSSRe.
// Proves sanitizer_recognition: DOMPurify.sanitize matches jstsSanitizerHTMLRe.
// Proves vulnerability_finding: source → sink without sanitizer in same fn.
function renderHtml(req: any) {
  const userId = req.body.userId;
  const rawContent = req.body.content;

  // Safe path: sanitize before render.
  const clean = DOMPurify.sanitize(rawContent);
  const safeEl = { __html: clean };

  // Unsafe path: unsanitised dangerouslySetInnerHTML (XSS vector).
  const unsafeEl = { dangerouslySetInnerHTML: { __html: rawContent } };

  return { safe: safeEl, unsafe: unsafeEl };
}

// Server-side route that also emits a response shape.
// Proves response_shape_extraction via res.json({...}).
function serverRoute(req: any, res: any) {
  const id = req.body.userId;
  res.json({ status: 'ok', userId: id });
}

// ── def_use_chain_extraction ──────────────────────────────────────────────────
// Proves def_use_chain_extraction: defines endpoint / result / parsed.
async function loadAndSync(userId: string) {
  const endpoint = 'https://api.example.com/sync';
  const result = await fetch(`${endpoint}/${userId}`);
  const parsed = await result.json();
  return parsed;
}

// ── template_pattern_catalog ──────────────────────────────────────────────────
// Proves template_pattern_catalog: i18n t(), log console.error, SQL literal.
function showScreen(label: string) {
  const title = t('home.title');
  console.error('Mobile error: %s', label);
  const query = `SELECT id, name FROM users WHERE active = 1`;
  return { title, query };
}

// ── request_shape_extraction ─────────────────────────────────────────────────
// Proves request_shape_extraction: axios body shape {amount, currency}.
async function postPayment(amount: number, currency: string) {
  return await axios.post('https://api.example.com/pay', { amount, currency });
}

// ── constant_propagation / env_fallback_recognition ──────────────────────────
const API_URL = 'https://api.example.com';
const RN_KEY = process.env.RN_API_KEY ?? 'dev-key';
const EXPO_API = import.meta.env.EXPO_PUBLIC_API_URL ?? 'https://fallback.example.com';
