// audit2853_js — Express slice. App-level global chain + per-route middleware.
import express from 'express'

const app = express()

function cors(req, res, next) { next() }
function requestLogger(req, res, next) { next() }
function rateLimit(req, res, next) { next() }
function validateQuery(req, res, next) { next() }
function validateBody(req, res, next) { next() }
function listUsers(req, res) { res.json([]) }
function createUser(req, res) { res.json({}) }
function healthCheck(req, res) { res.send('ok') }

app.use(cors())
app.use(express.json())
app.use(requestLogger)

app.get('/users', rateLimit, validateQuery, listUsers)
app.post('/users', validateBody, createUser)
app.get('/health', healthCheck)
