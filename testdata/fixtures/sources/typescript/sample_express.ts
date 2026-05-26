/**
 * Sample Express TypeScript application — golden fixture source.
 */
import express, { Request, Response, NextFunction } from "express";

interface User {
  id: number;
  name: string;
  email: string;
}

interface CreateUserBody {
  name: string;
  email: string;
}

const app = express();
app.use(express.json());

const users: User[] = [{ id: 1, name: "Alice", email: "alice@example.com" }];

function authMiddleware(req: Request, res: Response, next: NextFunction): void {
  const token = req.headers.authorization;
  if (!token) {
    res.status(401).json({ error: "Unauthorized" });
    return;
  }
  next();
}

app.get("/health", (req: Request, res: Response) => {
  res.json({ status: "ok" });
});

app.get("/users", authMiddleware, (req: Request, res: Response) => {
  res.json(users);
});

app.get("/users/:id", authMiddleware, (req: Request, res: Response) => {
  const id = parseInt(req.params.id, 10);
  const user = users.find((u) => u.id === id);
  if (!user) {
    res.status(404).json({ error: "Not found" });
    return;
  }
  res.json(user);
});

app.post("/users", authMiddleware, (req: Request, res: Response) => {
  const body = req.body as CreateUserBody;
  const newUser: User = { id: users.length + 1, ...body };
  users.push(newUser);
  res.status(201).json(newUser);
});

app.delete("/users/:id", authMiddleware, (req: Request, res: Response) => {
  const id = parseInt(req.params.id, 10);
  const index = users.findIndex((u) => u.id === id);
  if (index === -1) {
    res.status(404).json({ error: "Not found" });
    return;
  }
  users.splice(index, 1);
  res.status(204).send();
});

app.listen(8080, () => {
  console.log("Listening on :8080");
});

export default app;
