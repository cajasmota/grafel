// Fastify auth_coverage fixture (#2852). Hand-written.
import Fastify from 'fastify'

const fastify = Fastify()

// Route-level preHandler auth via a recognised guard middleware in the chain.
fastify.get('/account', requireAuth, getAccount)
fastify.post('/account', requireAuth, updateAccount)

// Public route.
fastify.get('/status', getStatus)
