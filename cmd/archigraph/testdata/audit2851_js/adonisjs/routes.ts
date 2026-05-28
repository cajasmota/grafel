// AdonisJS registration site. Controller actions are string references
// ('UsersController.index'); the controller class lives in UsersController.ts.
import Route from "@ioc:Adonis/Core/Route";

Route.get("/users", "UsersController.index");
Route.post("/users", "UsersController.store");
Route.get("/users/:id", "UsersController.show");
Route.resource("posts", "PostsController");
