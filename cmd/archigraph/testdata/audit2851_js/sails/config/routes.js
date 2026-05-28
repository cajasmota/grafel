// Sails declarative routes. Path-gated synthesizer fires on config/routes.js.
module.exports.routes = {
  'GET /widgets': 'WidgetController.find',
  'GET /widgets/:id': 'WidgetController.findOne',
  'POST /widgets': 'WidgetController.create',
  'DELETE /widgets/:id': 'WidgetController.destroy',
};
