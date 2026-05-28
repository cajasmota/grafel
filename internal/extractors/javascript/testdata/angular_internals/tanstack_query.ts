// Cross-framework TanStack Query — Angular adapter (#2910).
// @tanstack/angular-query-experimental exposes injectQuery / injectMutation /
// injectInfiniteQuery (the Angular equivalents of React's useQuery family).
import { Component } from '@angular/core';
import {
  injectQuery,
  injectMutation,
  injectInfiniteQuery,
} from '@tanstack/angular-query-experimental';

@Component({
  selector: 'app-todos',
  template: `<div>{{ todos.data() }}</div>`,
})
export class TodosComponent {
  todos = injectQuery(() => ({
    queryKey: ['todos'],
    queryFn: () => fetch('/api/todos').then((r) => r.json()),
  }));

  addTodo = injectMutation(() => ({
    mutationFn: (t: unknown) => fetch('/api/todos', { method: 'POST' }),
  }));

  feed = injectInfiniteQuery(() => ({
    queryKey: ['feed'],
    queryFn: ({ pageParam }) => fetch(`/api/feed?page=${pageParam}`),
    initialPageParam: 0,
    getNextPageParam: (last: any) => last.next,
  }));
}
