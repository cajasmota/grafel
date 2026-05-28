// Feathers middleware_coverage fixture (#2853). Hand-written, manifest-free.
// Feathers attaches middleware as service hooks (before/after/error) rather
// than per-route; hooks apply to every method of the mounted service.
import feathers from '@feathersjs/feathers'
import { MessageService } from './messages.service'

const app = feathers()

app.use('/messages', new MessageService())

app.service('messages').hooks({
  before: {
    all: [authenticate('jwt')],
    create: [validateData],
  },
  after: {
    all: [serialize],
  },
  error: {
    all: [logError],
  },
})
