// Hand-written Angular fixture proving issue #2874 (B) cells:
//   rxjs_pattern_detection + guard_interceptor_recognition.
// Dependency-manifest-free (no node_modules / lockfile) per the coverage
// fixture strategy — only the source idioms matter to the extractor.
import { Component, Injectable } from '@angular/core';
import {
  CanActivate,
  CanActivateChild,
  CanActivateFn,
  Resolve,
  Router,
} from '@angular/router';
import {
  HttpClient,
  HttpInterceptor,
  HttpInterceptorFn,
  HttpRequest,
  HttpHandler,
} from '@angular/common/http';
import { Observable, Subject, BehaviorSubject } from 'rxjs';
import { map, switchMap, filter, catchError, takeUntil } from 'rxjs/operators';

// --- rxjs_pattern_detection ---------------------------------------------
@Component({
  selector: 'app-feed',
  template: '<div>{{ feed$ | async }}</div>',
})
export class FeedComponent {
  private destroy$ = new Subject<void>();
  private countSubject = new BehaviorSubject<number>(0);
  feed$: Observable<string[]>;

  constructor(private http: HttpClient) {}

  load() {
    this.feed$ = this.http
      .get<string[]>('/api/feed')
      .pipe(
        map((rows) => rows),
        switchMap((rows) => this.http.get<string[]>('/api/more')),
        filter((rows) => rows.length > 0),
        catchError(() => []),
        takeUntil(this.destroy$),
      );
    this.feed$.subscribe((rows) => console.log(rows));
  }
}

// --- guard_interceptor_recognition (class forms) ------------------------
@Injectable({ providedIn: 'root' })
export class AuthGuard implements CanActivate, CanActivateChild {
  constructor(private router: Router) {}
  canActivate(): boolean {
    return true;
  }
  canActivateChild(): boolean {
    return true;
  }
}

@Injectable({ providedIn: 'root' })
export class FeedResolver implements Resolve<string[]> {
  constructor(private http: HttpClient) {}
  resolve(): Observable<string[]> {
    return this.http.get<string[]>('/api/feed');
  }
}

@Injectable()
export class AuthInterceptor implements HttpInterceptor {
  intercept(req: HttpRequest<unknown>, next: HttpHandler) {
    return next.handle(req);
  }
}

// --- guard_interceptor_recognition (functional forms) -------------------
export const adminGuard: CanActivateFn = (route, state) => {
  return true;
};

export const tokenInterceptor: HttpInterceptorFn = (req, next) => {
  return next(req);
};
