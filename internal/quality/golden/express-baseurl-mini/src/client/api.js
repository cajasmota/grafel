// Consumer side. `BASE` is imported, so the canonicaliser cannot see its
// value and emits path=/{BASE}/things with url_kind=dynamic_baseurl. Only
// the substrate constant fold turns that back into /api/things.
import { API_ORIGIN, BASE } from './config';

export async function listThings() {
  const res = await fetch(`${BASE}/things`);
  return res.json();
}

export async function checkHealth() {
  const res = await fetch(`${API_ORIGIN}/health`);
  return res.json();
}

export async function createThing(body) {
  const res = await fetch(`${BASE}/things`, {
    method: 'POST',
    body: JSON.stringify(body),
  });
  return res.json();
}
