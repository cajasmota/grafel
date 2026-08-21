// Producer side: a plain Express app mounting one route at a literal path.
// The consumer under ../client reaches this route through a base-URL
// constant declared in a DIFFERENT file, which is the shape #6450 is about.
const express = require('express');

const app = express();

app.get('/api/things', (req, res) => {
  res.json([{ id: 1 }]);
});

app.post('/api/things', (req, res) => {
  res.status(201).json({ id: 2 });
});

app.get('/api/health', (req, res) => {
  res.json({ ok: true });
});

module.exports = app;
