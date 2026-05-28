// Sails config/policies.js auth_coverage fixture (#2852, extended #2897).
// Hand-written, dependency-manifest-free.
//
// A protective global default ('*': 'isLoggedIn') gates every action unless a
// controller/action explicitly opts out with `true` (public). #2897 joins this
// map to the routes synthesised from config/routes.js and proves all three
// precedence levels:
//   - global '*': 'isLoggedIn' default protects every action,
//   - AuthController object block: per-action overrides (login public,
//     logout/signup gated),
//   - DashboardController bare value: a controller-level catch-all policy.
module.exports.policies = {
  '*': 'isLoggedIn',

  AuthController: {
    login: true,
    logout: 'isLoggedIn',
  },

  DashboardController: 'isLoggedIn',
}
