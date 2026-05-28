exports.up = (knex) => knex.schema.createTable('users', (t) => { t.increments('id'); t.string('email'); t.index(['email']) })
exports.down = (knex) => knex.schema.dropTable('users')
