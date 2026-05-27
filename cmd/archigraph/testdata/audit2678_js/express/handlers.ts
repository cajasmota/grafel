// Handler functions live HERE — endpoint attribution must resolve to this
// file, not to routes.ts where they're registered.

import type { Request, Response } from "express";

export function listUsers(req: Request, res: Response): void {
  res.json([{ id: 1, name: "alice" }]);
}

export function createUser(req: Request, res: Response): void {
  res.status(201).json({ ok: true });
}

export function getUser(req: Request, res: Response): void {
  res.json({ id: req.params.id });
}
