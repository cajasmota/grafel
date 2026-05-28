// Sails routing fixture (#2851, extended #2897). Represents a Sails
// config/routes.js map. Hand-written, dependency-manifest-free. The
// synthesizer is path-gated on the `config/routes.{js,ts}` filename, so the
// test feeds this content under that path.
//
// #2897 — the controller/action targets here are joined to config/policies.js:
//   UsersController.*    -> global '*': 'isLoggedIn' (protected, no override)
//   AuthController.login -> action-level override `true` (public)
//   AuthController.logout-> action-level 'isLoggedIn' (protected)
//   DashboardController.*-> controller-level 'isLoggedIn' (protected)
module.exports.routes = {
  'GET /users': 'UsersController.index',
  'GET /users/:id': 'UsersController.show',
  'POST /users': 'UsersController.create',
  'PUT /users/:id': 'UsersController.update',
  'DELETE /users/:id': 'UsersController.destroy',
  'POST /login': 'AuthController.login',
  'POST /logout': 'AuthController.logout',
  'GET /dashboard': 'DashboardController.index',
  '/health': 'SystemController.health',
}
