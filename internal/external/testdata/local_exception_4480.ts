// Faithful shape of a service that declares its own exception class and throws
// it — the locally-declared-exception case for #4480. The declared class is a
// real graph entity (SCOPE.Class); the synthetic SCOPE.ExceptionType must be
// resolved to it.

export class AppNotFoundError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'AppNotFoundError';
  }
}

export class WidgetService {
  findById(id: number): string {
    if (id <= 0) {
      throw new AppNotFoundError('widget not found');
    }
    return 'widget-' + id;
  }
}
