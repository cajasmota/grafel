// Polka auth_coverage fixture (#2852). Hand-written.
import polka from 'polka'

const app = polka()

// Route-level guard middleware (high confidence).
app.get('/private', requireAuth, getPrivate)

// Public route.
app.get('/public', getPublic)
