// Source: synthetic, modelled on real Angular 17 component data-flow patterns
// (@Input/@Output props, ngrx Store state, HttpClient fetch, *ngIf/@if
// branches) | License: MIT
//
// Used by issue #2855 real-data verification (Data Flow group): proves the
// Angular extractor emits component_prop / data_fetch / branch_condition
// entities and ngrx Store CALLS edges on real-shaped source.

import { Component, Input, Output, EventEmitter, OnInit } from '@angular/core';
import { HttpClient } from '@angular/common/http';
import { Store } from '@ngrx/store';
import { Observable } from 'rxjs';

import { loadUsers, deleteUser } from './user.actions';
import { selectUsers, selectLoading } from './user.selectors';
import { User } from './user.model';

@Component({
  selector: 'app-user-list',
  standalone: true,
  template: `
    <div class="user-list">
      <header *ngIf="title">{{ title }}</header>

      @if (loading$ | async) {
        <spinner-overlay></spinner-overlay>
      } @else {
        <user-card
          *ngFor="let user of users$ | async"
          [user]="user"
          (selected)="onSelected($event)">
        </user-card>
      }

      <button (click)="refresh()">Reload</button>
    </div>
  `,
})
export class UserListComponent implements OnInit {
  @Input() title = 'Users';
  @Input() pageSize = 20;
  @Output() selected = new EventEmitter<User>();
  @Output() removed = new EventEmitter<string>();

  users$: Observable<User[]> = this.store.select(selectUsers);
  loading$: Observable<boolean> = this.store.select(selectLoading);

  constructor(
    private store: Store,
    private http: HttpClient,
  ) {}

  ngOnInit(): void {
    this.store.dispatch(loadUsers({ pageSize: this.pageSize }));
  }

  refresh(): void {
    this.http.get<User[]>('/api/users').subscribe();
    this.store.dispatch(loadUsers({ pageSize: this.pageSize }));
  }

  remove(id: string): void {
    this.http.delete(`/api/users/${id}`).subscribe();
    this.store.dispatch(deleteUser({ id }));
    this.removed.emit(id);
  }

  onSelected(user: User): void {
    this.selected.emit(user);
  }
}
