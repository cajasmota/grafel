// Hapi registration site. Named handlers live in handlers.ts.
import Hapi from "@hapi/hapi";
import { listBooks, getBook } from "./handlers";

const server = Hapi.server({ port: 4000 });

server.route({
  method: "GET",
  path: "/books",
  handler: listBooks,
});

server.route({
  method: "GET",
  path: "/books/{id}",
  handler: getBook,
});

export default server;
