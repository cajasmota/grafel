// Polka middleware_coverage fixture (#2853). Hand-written, manifest-free.
// Polka is Express-shaped: app.use global chain + per-route middleware.
import polka from 'polka'

const app = polka()

// Global middleware chain.
app.use(compression())
app.use(requestLogger)

// Per-route middleware before the handler.
app.get('/private', requireAuth, getPrivate)
app.get('/public', getPublic)
