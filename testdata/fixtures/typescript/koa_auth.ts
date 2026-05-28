// Koa (koa-router) auth_coverage fixture (#2852). Hand-written.
import Router from '@koa/router'

const router = new Router()

// Route-level guard middleware (high confidence).
router.get('/profile', requireAuth, getProfile)
router.put('/profile', requireAuth, updateProfile)

// Public route — no auth middleware in the chain.
router.get('/ping', ping)
