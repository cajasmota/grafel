// Hono auth_coverage fixture (#2852). Hand-written.
import { Hono } from 'hono'

const app = new Hono()

// App-level JWT middleware → file-scope coverage.
app.use(jwtAuth)

// Route-level guard (high confidence).
app.get('/secure', verifyToken, getSecure)

// Inherits the app-level jwtAuth gate (medium).
app.get('/items', listItems)
