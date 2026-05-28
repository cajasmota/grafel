exports.up = (knex) => knex.schema.createTable('persons', (t) => { t.increments('id') })
exports.down = (knex) => knex.schema.dropTable('persons')
