// Express auth corpus file (#2852 real-data verification).
import express from 'express'
import passport from 'passport'

const app = express()
app.use(passport.authenticate('jwt', { session: false }))

function requireAuth(req, res, next) { next() }
function getAccount(req, res) { res.json({}) }
function updateAccount(req, res) { res.json({}) }
function ping(req, res) { res.send('ok') }

app.get('/account', requireAuth, getAccount)
app.post('/account', requireAuth, updateAccount)
app.get('/ping', ping)
