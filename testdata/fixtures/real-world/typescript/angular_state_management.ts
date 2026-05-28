// angular_state_management.ts — proving fixture for issue #2884.
//
// Mirrors the state idioms the independent audit (#2847) found on the
// gothinkster angular-realworld-example-app, where all stateful files use
// Angular signals + RxJS BehaviorSubject and ZERO use ngrx. The pre-#2884
// extractor only recognised ngrx Store, so it extracted no state here and the
// Data Flow/state_management cell was reverted full -> partial.
//
// This fixture exercises every supported idiom:
//   1. Angular signals  : signal()/computed() containers + .set()/.update()/.mutate()
//   2. RxJS subjects     : new BehaviorSubject(...) containers + .next() setters
//   3. ngrx signal store : signalStore()/withState() (modern ngrx, NOT Redux)
//   4. ngrx Redux Store  : this.store.select()/dispatch() (kept for regression)
import { Component, Injectable, signal, computed } from '@angular/core';
import { BehaviorSubject } from 'rxjs';
import { Store } from '@ngrx/store';
import { signalStore, withState } from '@ngrx/signals';

// --- signals: modern Angular default (matches auth.component.ts) ------------
@Component({ selector: 'app-auth', template: '<form></form>' })
export class AuthComponent {
  isSubmitting = signal(false);
  errors = signal<{ errors: object }>({ errors: {} });
  // computed: derived signal state.
  hasErrors = computed(() => Object.keys(this.errors().errors).length > 0);

  submit(): void {
    this.isSubmitting.set(true);
    this.errors.set({ errors: {} });
    this.isSubmitting.update((v) => !v);
  }
}

// --- RxJS BehaviorSubject service state (matches user.service.ts) -----------
@Injectable({ providedIn: 'root' })
export class UserService {
  private currentUserSubject = new BehaviorSubject<object | null>(null);
  private authStateSubject = new BehaviorSubject<string>('loading');

  setAuth(user: object): void {
    this.currentUserSubject.next(user);
    this.authStateSubject.next('authenticated');
  }

  purgeAuth(): void {
    this.currentUserSubject.next(null);
    this.authStateSubject.next('unauthenticated');
  }
}

// --- ngrx signal store (modern ngrx) ---------------------------------------
@Injectable({ providedIn: 'root' })
export class CounterStore {
  store = signalStore(withState({ count: 0 }));
}

// --- ngrx Redux Store (legacy path kept for regression) ---------------------
@Component({ selector: 'app-counter', template: '<div></div>' })
export class CounterComponent {
  constructor(private store: Store) {}

  load(): void {
    this.store.select((s: { count: number }) => s.count);
    this.store.dispatch(increment());
  }
}

declare function increment(): object;
