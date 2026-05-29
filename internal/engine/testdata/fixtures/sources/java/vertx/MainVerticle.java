package com.example;

import io.vertx.core.AbstractVerticle;
import io.vertx.core.Promise;
import io.vertx.ext.web.Router;
import io.vertx.ext.web.handler.BodyHandler;
import io.vertx.ext.web.handler.CorsHandler;
import io.vertx.ext.web.handler.JWTAuthHandler;
import io.vertx.ext.web.handler.LoggerHandler;
import io.vertx.ext.auth.jwt.JWTAuth;
import com.example.dto.CreateUserRequest;
import com.example.dto.UpdateUserRequest;

/**
 * Main Vert.x Web verticle — used as a fixture for the Vert.x route extractor
 * test suite (internal/custom/java/vertx_routes.go, issue #3086).
 *
 * Demonstrates:
 *  - Router lambda DSL: router.get("/path").handler(ctx -> ...)
 *  - Method-reference handlers: router.get("/path").handler(Handler::method)
 *  - Global middleware: router.route().handler(BodyHandler.create())
 *  - JWT auth: router.route("/api/*").handler(JWTAuthHandler.create(jwt))
 *  - DTO body mapping: ctx.body().as(CreateUserRequest.class)
 *  - Path parameters: /users/:id (colon style, Vert.x default)
 */
public class MainVerticle extends AbstractVerticle {

    private JWTAuth jwtProvider;
    private UserService userService;

    @Override
    public void start(Promise<Void> startPromise) {
        jwtProvider = createJwtProvider();
        userService = new UserService();

        Router router = Router.router(vertx);

        // ----------------------------------------------------------------
        // Global middleware
        // ----------------------------------------------------------------
        router.route().handler(BodyHandler.create());
        router.route().handler(LoggerHandler.create());
        router.route().handler(CorsHandler.create("*")
            .allowedMethod(io.vertx.core.http.HttpMethod.GET)
            .allowedMethod(io.vertx.core.http.HttpMethod.POST));

        // ----------------------------------------------------------------
        // JWT authentication for /api/* routes
        // ----------------------------------------------------------------
        router.route("/api/*").handler(JWTAuthHandler.create(jwtProvider));

        // ----------------------------------------------------------------
        // Public routes
        // ----------------------------------------------------------------
        router.get("/health").handler(ctx ->
            ctx.response().setStatusCode(200).end("UP"));

        router.get("/version").handler(ctx ->
            ctx.response().end("{\"version\":\"1.0.0\"}"));

        // ----------------------------------------------------------------
        // User CRUD routes (authenticated)
        // ----------------------------------------------------------------
        router.get("/api/users").handler(UserHandler::listAll);

        router.post("/api/users").handler(ctx -> {
            CreateUserRequest req = ctx.body().as(CreateUserRequest.class);
            userService.create(req)
                .onSuccess(user -> ctx.response().setStatusCode(201).end(user.toJson().encode()))
                .onFailure(err -> ctx.fail(500, err));
        });

        router.get("/api/users/:id").handler(UserHandler::getById);

        router.put("/api/users/:id").handler(ctx -> {
            String id = ctx.pathParam("id");
            UpdateUserRequest req = ctx.body().as(UpdateUserRequest.class);
            userService.update(id, req)
                .onSuccess(updated -> ctx.response().end(updated.toJson().encode()))
                .onFailure(err -> ctx.fail(404, err));
        });

        router.delete("/api/users/:id").handler(UserHandler::delete);

        // ----------------------------------------------------------------
        // Nested resource routes
        // ----------------------------------------------------------------
        router.get("/api/users/:userId/posts").handler(ctx -> {
            String userId = ctx.pathParam("userId");
            postService.findByUser(userId)
                .onSuccess(posts -> ctx.response().end(posts.encode()));
        });

        router.get("/api/users/:userId/posts/:postId").handler(ctx -> {
            ctx.response().end("post");
        });

        // ----------------------------------------------------------------
        // Start HTTP server
        // ----------------------------------------------------------------
        vertx.createHttpServer()
            .requestHandler(router)
            .listen(8080, http -> {
                if (http.succeeded()) {
                    startPromise.complete();
                } else {
                    startPromise.fail(http.cause());
                }
            });
    }

    private JWTAuth createJwtProvider() {
        // JWT provider configuration (simplified)
        return JWTAuth.create(vertx, new io.vertx.ext.auth.jwt.JWTAuthOptions());
    }
}
