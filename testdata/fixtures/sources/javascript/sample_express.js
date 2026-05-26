/**
 * Sample Express JavaScript application — golden fixture source.
 */
const express = require("express");

const app = express();
app.use(express.json());

const users = [{ id: 1, name: "Alice", email: "alice@example.com" }];

function validateUser(req, res, next) {
  if (!req.body.name || !req.body.email) {
    return res.status(400).json({ error: "name and email required" });
  }
  next();
}

app.get("/health", (req, res) => {
  res.json({ status: "ok" });
});

app.get("/users", (req, res) => {
  res.json(users);
});

app.get("/users/:id", (req, res) => {
  const id = parseInt(req.params.id, 10);
  const user = users.find((u) => u.id === id);
  if (!user) {
    return res.status(404).json({ error: "Not found" });
  }
  res.json(user);
});

app.post("/users", validateUser, (req, res) => {
  const newUser = { id: users.length + 1, ...req.body };
  users.push(newUser);
  res.status(201).json(newUser);
});

app.delete("/users/:id", (req, res) => {
  const id = parseInt(req.params.id, 10);
  const index = users.findIndex((u) => u.id === id);
  if (index === -1) {
    return res.status(404).json({ error: "Not found" });
  }
  users.splice(index, 1);
  res.status(204).send();
});

app.listen(8080, () => {
  console.log("Listening on :8080");
});

module.exports = app;
