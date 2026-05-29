package com.example.javalin;

import io.javalin.Javalin;
import io.javalin.security.AccessManager;
import io.javalin.security.RouteRole;
import com.example.javalin.dto.CreateUserRequest;
import com.example.javalin.dto.UserResponse;
import com.example.javalin.dto.CreateOrderRequest;
import com.example.javalin.controller.UserController;

/**
 * Javalin sample application — fixture for #3085.
 *
 * Demonstrates:
 *   - Lambda DSL route registration (route_extraction, endpoint_synthesis)
 *   - Handler attribution via lambda param and method reference
 *   - Before/after middleware (middleware_coverage)
 *   - AccessManager for role-based auth (auth_coverage)
 *   - ctx.bodyAsClass and ctx.bodyValidator for DTO extraction
 *   - Path parameters in {param} curly-brace style
 */
public class App {

    enum Role implements RouteRole { ADMIN, USER, ANYONE }

    public static Javalin createApp() {
        return Javalin.create(config -> {
            // AccessManager: centralised role-based auth hook
            config.accessManager((handler, ctx, permittedRoles) -> {
                if (permittedRoles.contains(Role.ANYONE)) {
                    handler.handle(ctx);
                } else {
                    User user = ctx.attribute("user");
                    if (user == null) {
                        ctx.status(401).result("Unauthorized");
                    } else if (permittedRoles.contains(user.getRole())) {
                        handler.handle(ctx);
                    } else {
                        ctx.status(403).result("Forbidden");
                    }
                }
            });
        });
    }

    public static void main(String[] args) {
        var app = createApp().start(7070);

        // ---------------------------------------------------------------------------
        // Middleware: before/after handlers (middleware_coverage)
        // ---------------------------------------------------------------------------

        // Global before handler — request logging
        app.before(ctx -> {
            System.out.println("[REQ] " + ctx.method() + " " + ctx.path());
        });

        // Path-scoped before handler — API key validation
        app.before("/api/*", ctx -> {
            String apiKey = ctx.header("X-Api-Key");
            if (apiKey == null || !isValidApiKey(apiKey)) {
                ctx.status(401).result("Invalid API key");
            }
        });

        // Global after handler — response time header
        app.after(ctx -> {
            ctx.header("X-Powered-By", "Javalin");
        });

        // ---------------------------------------------------------------------------
        // Public routes (no auth)
        // ---------------------------------------------------------------------------

        app.get("/health", ctx -> ctx.json("{\"status\":\"UP\"}"), Role.ANYONE);

        app.get("/version", ctx -> ctx.result("1.0.0"), Role.ANYONE);

        // ---------------------------------------------------------------------------
        // User routes — lambda DSL (route_extraction + endpoint_synthesis)
        // ---------------------------------------------------------------------------

        app.get("/users", ctx -> {
            ctx.json(userService.findAll());
        }, Role.USER);

        app.post("/users", ctx -> {
            // DTO extraction: ctx.bodyAsClass (dto_extraction)
            var req = ctx.bodyAsClass(CreateUserRequest.class);
            var created = userService.create(req);
            ctx.status(201).json(created);
        }, Role.USER);

        app.get("/users/{id}", ctx -> {
            String id = ctx.pathParam("id");
            ctx.json(userService.findById(id));
        }, Role.USER);

        app.put("/users/{id}", ctx -> {
            // Request validation: ctx.bodyValidator (request_validation)
            var req = ctx.bodyValidator(CreateUserRequest.class)
                .check(it -> it.getName() != null && !it.getName().isEmpty(), "Name is required")
                .check(it -> it.getEmail() != null, "Email is required")
                .get();
            userService.update(ctx.pathParam("id"), req);
            ctx.status(200);
        }, Role.USER);

        app.delete("/users/{id}", ctx -> {
            userService.delete(ctx.pathParam("id"));
            ctx.status(204);
        }, Role.ADMIN);

        // ---------------------------------------------------------------------------
        // Handler attribution via method reference (handler_attribution)
        // ---------------------------------------------------------------------------

        app.get("/orders", UserController::listOrders);
        app.post("/orders", UserController::createOrder);
        app.get("/orders/{orderId}", UserController::getOrder);

        // ---------------------------------------------------------------------------
        // Order management (DTO extraction)
        // ---------------------------------------------------------------------------

        app.put("/orders/{orderId}", ctx -> {
            var req = ctx.bodyAsClass(CreateOrderRequest.class);
            orderService.update(ctx.pathParam("orderId"), req);
        }, Role.USER);

        app.delete("/orders/{orderId}", ctx -> {
            orderService.cancel(ctx.pathParam("orderId"));
            ctx.status(204);
        }, Role.USER);

        // ---------------------------------------------------------------------------
        // Admin routes (ADMIN role required)
        // ---------------------------------------------------------------------------

        app.get("/admin/stats", ctx -> {
            ctx.json(statsService.getAll());
        }, Role.ADMIN);

        app.delete("/admin/cache", ctx -> {
            cacheService.evictAll();
            ctx.status(200).result("Cache cleared");
        }, Role.ADMIN);

        // ---------------------------------------------------------------------------
        // Error handlers
        // ---------------------------------------------------------------------------

        app.error(404, ctx -> ctx.result("Resource not found"));
        app.error(500, ctx -> ctx.result("Internal server error"));
    }

    private static boolean isValidApiKey(String key) {
        return key != null && key.startsWith("ak_");
    }
}
