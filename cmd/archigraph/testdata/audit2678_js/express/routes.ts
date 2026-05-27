// Registration site — the BUG (#2678) was that endpoints attributed
// source_file to THIS file even though listUsers/createUser/getUser live
// in handlers.ts.

import express from "express";
import { listUsers, createUser, getUser } from "./handlers";

const app = express();
const router = express.Router();

router.get("/users", listUsers);
router.post("/users", createUser);
router.get("/users/:id", getUser);

app.use("/api", router);

export default app;
