// Polka handler functions live HERE. Endpoint attribution must resolve to
// this file, not to server.ts where they are registered.

export function listUsers(req, res) {
  res.end(JSON.stringify([{ id: 1 }]));
}

export function getUser(req, res) {
  res.end(JSON.stringify({ id: req.params.id }));
}

export function createUser(req, res) {
  res.statusCode = 201;
  res.end("{}");
}
