// audit2853_js — Fastify slice. Global hooks (addHook) + per-route hook chain.
import Fastify from 'fastify'

const fastify = Fastify()

function authenticate(req, reply, done) { done() }
function validate(req, reply, done) { done() }
function preHandlerGuard(req, reply, done) { done() }
function getAccount(req, reply) { reply.send({}) }
function getStatus(req, reply) { reply.send('ok') }

fastify.addHook('onRequest', authenticate)
fastify.addHook('preHandler', validate)

fastify.get('/account', preHandlerGuard, getAccount)
fastify.get('/status', getStatus)
