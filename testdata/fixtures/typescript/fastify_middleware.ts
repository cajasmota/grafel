// Fastify middleware_coverage fixture (#2853). Hand-written, manifest-free.
// Fastify expresses middleware through global hooks (addHook) and per-route
// hook options (preHandler/onRequest).
import Fastify from 'fastify'

const fastify = Fastify()

// Global lifecycle hooks — apply to every route.
fastify.addHook('onRequest', authenticate)
fastify.addHook('preHandler', validate)

// Per-route hook chain.
fastify.get('/account', preHandlerGuard, getAccount)
fastify.post('/account', validateBody, createAccount)
fastify.get('/status', getStatus)
