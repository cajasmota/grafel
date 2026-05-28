// audit2853_js — Koa slice. app.use global chain + koa-router per-route mw.
import Koa from 'koa'
import Router from '@koa/router'

const app = new Koa()
const router = new Router()

function bodyParser() { return async (ctx, next) => next() }
function requestLogger(ctx, next) { return next() }
function rateLimit(ctx, next) { return next() }
function getProfile(ctx) { ctx.body = {} }
function ping(ctx) { ctx.body = 'ok' }

app.use(bodyParser())
app.use(requestLogger)

router.get('/profile', rateLimit, getProfile)
router.get('/ping', ping)
