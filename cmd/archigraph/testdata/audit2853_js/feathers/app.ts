// audit2853_js — Feathers slice. Service hooks (before/after/error).
import feathers from '@feathersjs/feathers'
import { MessageService } from './messages.service'

const app = feathers()

app.use('/messages', new MessageService())

app.service('messages').hooks({
  before: { all: [authenticate('jwt')], create: [validateData] },
  after: { all: [serialize] },
  error: { all: [logError] },
})
