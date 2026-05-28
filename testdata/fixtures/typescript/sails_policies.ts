// Sails config/policies.js auth_coverage fixture (#2852). Hand-written.
// A protective global default ('*': 'isLoggedIn') gates every action unless a
// controller/action explicitly opts out with `true` (public).
module.exports.policies = {
  '*': 'isLoggedIn',

  AuthController: {
    login: true,
    logout: 'isLoggedIn',
  },
}
