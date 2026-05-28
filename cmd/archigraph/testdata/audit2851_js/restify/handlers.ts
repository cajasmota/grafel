// Restify handler functions live HERE. Endpoint attribution must resolve to
// this file, not to server.ts where they are registered.

export function listItems(req, res, next) {
  res.send([{ id: 1 }]);
  next();
}

export function getItem(req, res, next) {
  res.send({ id: req.params.id });
  next();
}

export function removeItem(req, res, next) {
  res.send(204);
  next();
}
