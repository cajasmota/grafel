// Sails controller. Actions back the config/routes.js 'WidgetController.x' refs.
module.exports = {
  find: function (req, res) {
    return res.json([{ id: 1 }]);
  },
  findOne: function (req, res) {
    return res.json({ id: req.params.id });
  },
  create: function (req, res) {
    return res.status(201).json({});
  },
  destroy: function (req, res) {
    return res.send(204);
  },
};
