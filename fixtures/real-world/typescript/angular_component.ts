// Source: https://github.com/angular/angular (synthetic based on real Angular 17+ standalone component patterns) | License: MIT

import {
  Component,
  OnInit,
  OnDestroy,
  inject,
  signal,
  computed,
  DestroyRef,
} from '@angular/core';
import { CommonModule } from '@angular/common';
import { RouterLink } from '@angular/router';
import { FormsModule } from '@angular/forms';
import { takeUntilDestroyed, toSignal } from '@angular/core/rxjs-interop';
import { Subject, debounceTime, distinctUntilChanged, switchMap, catchError, of } from 'rxjs';
import { PostService } from '../services/post.service';
import { AuthService } from '../services/auth.service';
import { Post, PaginatedResponse } from '../models/post.model';

@Component({
  selector: 'app-post-list',
  standalone: true,
  imports: [CommonModule, RouterLink, FormsModule],
  template: `
    <div class="post-list">
      <div class="filters">
        <input
          type="search"
          placeholder="Search posts..."
          [(ngModel)]="searchQuery"
          (ngModelChange)="onSearch($event)"
          class="search-input"
          aria-label="Search posts"
        />
        <select [(ngModel)]="selectedStatus" (ngModelChange)="loadPosts()">
          <option value="">All</option>
          <option value="published">Published</option>
          <option value="draft">Draft</option>
        </select>
      </div>

      @if (isLoading()) {
        <div class="loading" aria-live="polite">
          <span class="spinner"></span> Loading...
        </div>
      } @else if (error()) {
        <div class="error" role="alert">
          {{ error() }}
          <button (click)="loadPosts()">Retry</button>
        </div>
      } @else {
        <ul class="posts">
          @for (post of posts(); track post.id) {
            <li class="post-card">
              @if (post.coverImage) {
                <img [src]="post.coverImage" [alt]="post.title" loading="lazy" />
              }
              <div class="content">
                <h2>
                  <a [routerLink]="['/posts', post.slug]">{{ post.title }}</a>
                </h2>
                <p>{{ post.excerpt }}</p>
                <div class="meta">
                  <span>{{ post.author.name }}</span>
                  <time [dateTime]="post.publishedAt | date:'yyyy-MM-dd'">
                    {{ post.publishedAt | date:'mediumDate' }}
                  </time>
                  @for (tag of post.tags; track tag.id) {
                    <span class="tag">{{ tag.name }}</span>
                  }
                </div>
              </div>
              @if (canEdit(post)) {
                <div class="actions">
                  <a [routerLink]="['/posts', post.id, 'edit']" class="btn">Edit</a>
                  <button (click)="confirmDelete(post)" class="btn-danger">Delete</button>
                </div>
              }
            </li>
          } @empty {
            <li class="empty">No posts found.</li>
          }
        </ul>

        @if (pagination().totalPages > 1) {
          <div class="pagination">
            <button [disabled]="currentPage() === 1" (click)="changePage(currentPage() - 1)">
              Previous
            </button>
            <span>Page {{ currentPage() }} of {{ pagination().totalPages }}</span>
            <button [disabled]="currentPage() === pagination().totalPages" (click)="changePage(currentPage() + 1)">
              Next
            </button>
          </div>
        }
      }

      @if (postToDelete()) {
        <div class="modal-overlay">
          <div class="modal">
            <p>Delete "{{ postToDelete()?.title }}"?</p>
            <button (click)="deletePost()">Confirm</button>
            <button (click)="postToDelete.set(null)">Cancel</button>
          </div>
        </div>
      }
    </div>
  `,
  styles: [`
    .post-list { max-width: 800px; margin: 0 auto; padding: 1rem; }
    .filters { display: flex; gap: 1rem; margin-bottom: 1.5rem; }
    .search-input { flex: 1; padding: 0.5rem; }
    .posts { list-style: none; padding: 0; }
    .post-card { display: flex; gap: 1rem; padding: 1.5rem; border-bottom: 1px solid #eee; }
    .meta { font-size: 0.875rem; color: #666; margin-top: 0.5rem; }
    .tag { background: #f0f0f0; padding: 0.1rem 0.4rem; border-radius: 4px; margin-left: 0.25rem; }
    .modal-overlay { position: fixed; inset: 0; background: rgba(0,0,0,.5); display: flex; align-items: center; justify-content: center; }
    .modal { background: white; padding: 2rem; border-radius: 8px; }
  `],
})
export class PostListComponent implements OnInit {
  private postService = inject(PostService);
  private authService = inject(AuthService);
  private destroyRef = inject(DestroyRef);

  // Signals
  posts = signal<Post[]>([]);
  isLoading = signal(false);
  error = signal<string | null>(null);
  currentPage = signal(1);
  pagination = signal({ totalPages: 1, totalCount: 0 });
  postToDelete = signal<Post | null>(null);

  searchQuery = '';
  selectedStatus = '';

  private searchSubject = new Subject<string>();
  currentUser = toSignal(this.authService.currentUser$);

  canEdit(post: Post) {
    return this.currentUser()?.id === post.author.id || this.currentUser()?.role === 'admin';
  }

  ngOnInit() {
    this.searchSubject.pipe(
      debounceTime(300),
      distinctUntilChanged(),
      switchMap(() => this.doFetch()),
      takeUntilDestroyed(this.destroyRef),
    ).subscribe();

    this.loadPosts();
  }

  onSearch(query: string) {
    this.searchSubject.next(query);
  }

  loadPosts() {
    this.doFetch().pipe(takeUntilDestroyed(this.destroyRef)).subscribe();
  }

  private doFetch() {
    this.isLoading.set(true);
    this.error.set(null);
    return this.postService.getPosts({
      search: this.searchQuery,
      status: this.selectedStatus,
      page: this.currentPage(),
    }).pipe(
      catchError(err => {
        this.error.set(err.message ?? 'Failed to load posts');
        return of({ items: [], totalCount: 0, totalPages: 1 });
      }),
    );
  }

  changePage(page: number) {
    this.currentPage.set(page);
    this.loadPosts();
  }

  confirmDelete(post: Post) {
    this.postToDelete.set(post);
  }

  async deletePost() {
    const post = this.postToDelete();
    if (!post) return;
    this.postToDelete.set(null);
    await this.postService.deletePost(post.id).toPromise();
    this.posts.update(p => p.filter(x => x.id !== post.id));
  }
}
