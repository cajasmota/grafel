// Hono middleware_coverage fixture (#2853). Hand-written, manifest-free.
// Hono uses app.use for global middleware and per-route middleware args.
import { Hono } from 'hono'

const app = new Hono()

// App-level global middleware.
app.use(logger())
app.use(prettyJSON())

// Per-route middleware before the handler.
app.get('/secure', verifyToken, getSecure)
app.get('/items', cacheControl, listItems)
