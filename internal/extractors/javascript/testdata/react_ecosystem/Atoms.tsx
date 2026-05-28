// Atoms.tsx — proving fixture for issue #2894 PR2 React Ecosystem group:
// atom_store_extraction. Mixes Recoil (atom/selector), Jotai (atom/
// atomWithStorage), Valtio (proxy) and MobX (makeAutoObservable/observable)
// store declarations. Read/write hooks (useRecoilState/useAtom/useSnapshot)
// surface as USES_HOOK via the generic hook pass.
import { atom, selector, useRecoilState, useRecoilValue } from 'recoil';
import { atom as jotaiAtom, atomWithStorage, useAtom, useAtomValue } from 'jotai';
import { proxy, useSnapshot } from 'valtio';
import { makeAutoObservable, observable } from 'mobx';

// --- Recoil atom + selector (atom_store_extraction) ---
export const countState = atom({
  key: 'countState',
  default: 0,
});

export const doubledState = selector({
  key: 'doubledState',
  get: ({ get }) => get(countState) * 2,
});

// --- Jotai atoms (atom_store_extraction) ---
export const tokenAtom = jotaiAtom<string | null>(null);
export const prefsAtom = atomWithStorage('prefs', { theme: 'light' });

// --- Valtio proxy store (atom_store_extraction) ---
export const cartStore = proxy({ items: [] as string[], total: 0 });

// --- MobX observable store (atom_store_extraction) ---
export const counterStore = observable({ value: 0 });

class TodoStore {
  todos: string[] = [];
  constructor() {
    makeAutoObservable(this);
  }
}
export const todoStore = new TodoStore();

// --- read/write components (USES_HOOK via generic pass) ---
export function Counter() {
  const [count, setCount] = useRecoilState(countState);
  const doubled = useRecoilValue(doubledState);
  const [token] = useAtom(tokenAtom);
  const prefs = useAtomValue(prefsAtom);
  const snap = useSnapshot(cartStore);
  return null;
}
