const { Model } = require('objection')
class Person extends Model {
  static get tableName() { return 'persons' }
  static get jsonSchema() { return { type: 'object', properties: { name: { type: 'string' } } } }
  static get relationMappings() { return { pets: { relation: Model.HasManyRelation, modelClass: Pet } } }
}
