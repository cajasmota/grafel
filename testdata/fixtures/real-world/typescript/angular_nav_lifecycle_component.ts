// Source: synthetic, modelled on real Angular 17 component navigation +
// lifecycle patterns (Router.navigate/navigateByUrl, routerLink template
// directive, RouterModule route table, signal().set/.update setters, ngrx
// Store.dispatch) | License: MIT
//
// Used by issue #2856 real-data verification:
//   - Navigation / router_pattern        : NAVIGATES_TO edges from Router calls,
//     routerLink directives, and RouterModule.forRoot route declarations.
//   - Lifecycle / state_setter_emission  : state_setter operations + WRITES_TO
//     edges from signal .set/.update and ngrx dispatch.

import { Component, NgModule, signal } from '@angular/core';
import { Router, RouterModule, Routes } from '@angular/router';
import { Store } from '@ngrx/store';

import { loadUser, clearUser } from './user.actions';

@Component({
  selector: 'app-user-nav',
  standalone: false,
  template: `
    <nav>
      <a routerLink="/dashboard">Dashboard</a>
      <a [routerLink]="['/users', userId]">Profile</a>
      <button (click)="open()">Open settings</button>
      <span>{{ count() }}</span>
    </nav>
  `,
})
export class UserNavComponent {
  count = signal(0);
  userId = signal<string>('');

  constructor(
    private router: Router,
    private store: Store,
  ) {}

  open(): void {
    this.router.navigate(['/settings']);
    this.router.navigateByUrl('/profile');
  }

  inc(): void {
    this.count.set(1);
    this.count.update((c) => c + 1);
  }

  setUser(id: string): void {
    this.userId.set(id);
    this.store.dispatch(loadUser({ id }));
  }

  reset(): void {
    this.store.dispatch(clearUser());
  }
}

const routes: Routes = [
  { path: 'dashboard', component: UserNavComponent },
  { path: 'users/:id', component: UserNavComponent },
  { path: 'settings', component: UserNavComponent },
];

@NgModule({
  imports: [RouterModule.forRoot(routes)],
  exports: [RouterModule],
})
export class AppRoutingModule {}
