// Koa middleware_coverage fixture (#2853). Hand-written, manifest-free.
// Koa shares the Express-shaped router: app.use global chain + koa-router
// per-route middleware.
import Koa from 'koa'
import Router from '@koa/router'

const app = new Koa()
const router = new Router()

// App-level global middleware.
app.use(bodyParser())
app.use(requestLogger)

// koa-router per-route middleware chain.
router.get('/profile', rateLimit, getProfile)
router.put('/profile', validateBody, updateProfile)
router.get('/ping', ping)
