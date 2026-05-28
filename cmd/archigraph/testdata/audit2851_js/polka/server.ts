// Polka registration site. Handlers live in handlers.ts.
import polka from "polka";
import { listUsers, getUser, createUser } from "./handlers";

const app = polka();
app.get("/users", listUsers);
app.get("/users/:id", getUser);
app.post("/users", createUser);
app.listen(3000);

export default app;
