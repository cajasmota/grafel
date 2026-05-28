// Restify registration site. Handlers live in handlers.ts.
import restify from "restify";
import { listItems, getItem, removeItem } from "./handlers";

const server = restify.createServer();
server.get("/items", listItems);
server.get("/items/:id", getItem);
server.del("/items/:id", removeItem);
server.listen(8080);

export default server;
