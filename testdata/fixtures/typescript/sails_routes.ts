// Sails routing fixture (#2851). Represents a Sails config/routes.js map.
// Hand-written, dependency-manifest-free. The synthesizer is path-gated on
// the `config/routes.{js,ts}` filename, so the test feeds this content under
// that path.
module.exports.routes = {
  'GET /users': 'UsersController.index',
  'GET /users/:id': 'UsersController.show',
  'POST /users': 'UsersController.create',
  'PUT /users/:id': 'UsersController.update',
  'DELETE /users/:id': 'UsersController.destroy',
  '/health': 'SystemController.health',
}
