// Express middleware_coverage fixture (#2853). Hand-written, manifest-free.
// Covers app-level global chain (app.use) + per-route middleware arrays.
import express from 'express'

const app = express()

// App-level global middleware chain — applies to every endpoint in scope.
app.use(cors())
app.use(express.json())
app.use(requestLogger)

// Route with a per-route middleware chain before the handler.
app.get('/users', rateLimit, validateQuery, listUsers)
app.post('/users', validateBody, createUser)

// Route with no per-route middleware — inherits only the app-level chain.
app.get('/health', healthCheck)
