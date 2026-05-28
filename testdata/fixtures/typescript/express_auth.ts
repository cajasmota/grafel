// Express auth_coverage fixture (#2852). Hand-written, dependency-manifest-free.
// Mixes app-level passport, route-level requireAuth, and an unprotected route.
import express from 'express'
import passport from 'passport'

const app = express()

// App-level auth middleware → file-scope (medium) coverage for endpoints with
// no stronger route-level signal.
app.use(passport.authenticate('jwt', { session: false }))

// Route-level auth middleware (high confidence).
app.get('/me', requireAuth, getMe)
app.post('/orders', ensureAuthenticated, createOrder)

// Public health check — relies only on the app-level passport gate (medium).
app.get('/health', healthCheck)
