// Restify middleware_coverage fixture (#2853). Hand-written, manifest-free.
// Restify is Express-shaped: server.use global chain + per-route middleware.
import restify from 'restify'

const server = restify.createServer()

// Global middleware chain.
server.use(restify.plugins.bodyParser())
server.use(requestLogger)

// Per-route middleware before the handler.
server.get('/secrets', requireAuth, getSecrets)
server.get('/info', getInfo)
