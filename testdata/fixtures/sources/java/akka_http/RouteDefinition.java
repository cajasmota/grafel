package com.example.akkahttp;

import akka.http.javadsl.server.AllDirectives;
import akka.http.javadsl.server.Route;
import akka.http.javadsl.model.StatusCodes;

import java.time.Duration;
import java.util.Optional;

/**
 * Akka-HTTP Java DSL sample application — fixture for #3092.
 *
 * Demonstrates:
 *   - Directive-based route registration: path(), pathPrefix() (route_extraction, endpoint_synthesis)
 *   - HTTP method directives: get(), post(), put(), delete() (route_extraction)
 *   - Handler attribution via method references and lambda directives (handler_attribution)
 *   - Middleware-equivalent directives: handleExceptions(), withRequestTimeout(), logRequest()
 *     (middleware_coverage)
 *   - Auth directives: authenticateBasic(), authenticateOAuth2(), authorize() (auth_coverage)
 *   - DTO body extraction: entity(as(MyDto.class)) (dto_extraction)
 *   - Request parameter extraction: parameter(), parameterOptional() (request_validation)
 *   - Path parameters via pathPrefix/segment composition (route_extraction)
 *
 * NOT applicable for Akka-HTTP Java DSL:
 *   - DI (di_binding_extraction, di_injection_point, di_scope_resolution): Akka-HTTP has
 *     no built-in DI framework; projects wire dependencies manually or via a separate DI
 *     library (e.g. Guice, but that is not part of Akka-HTTP itself).
 *   - AOP (advice_attribution, aspect_extraction, pointcut_resolution): No AOP support.
 *   - Transactions (transaction_boundary_extraction, transaction_propagation,
 *     transaction_rollback_rules): HTTP routing layer; no transaction management.
 */
public class RouteDefinition extends AllDirectives {

    private final UserService userService;
    private final OrderService orderService;

    public RouteDefinition(UserService userService, OrderService orderService) {
        this.userService = userService;
        this.orderService = orderService;
    }

    // -------------------------------------------------------------------------
    // Exception handler middleware — handleExceptions() directive
    // -------------------------------------------------------------------------
    private static final akka.http.javadsl.server.ExceptionHandler exHandler =
        akka.http.javadsl.server.ExceptionHandler.newBuilder()
            .match(IllegalArgumentException.class, ex ->
                complete(StatusCodes.BAD_REQUEST, ex.getMessage()))
            .match(RuntimeException.class, ex ->
                complete(StatusCodes.INTERNAL_SERVER_ERROR, ex.getMessage()))
            .build();

    // -------------------------------------------------------------------------
    // Top-level route composition
    // -------------------------------------------------------------------------
    public Route createRoute() {
        return handleExceptions(exHandler, () ->
            withRequestTimeout(Duration.ofSeconds(30), () ->
                logRequest("akka-sample", () ->
                    concat(
                        userRoutes(),
                        orderRoutes(),
                        adminRoutes()
                    )
                )
            )
        );
    }

    // -------------------------------------------------------------------------
    // User routes: path() + HTTP method directives
    // -------------------------------------------------------------------------
    private Route userRoutes() {
        return pathPrefix("users", () ->
            concat(
                // GET /users — list all users
                pathEnd(() ->
                    get(() ->
                        complete(userService.list())
                    )
                ),
                // POST /users — create a user (DTO body extraction)
                pathEnd(() ->
                    post(() ->
                        entity(as(CreateUserRequest.class), req -> {
                            userService.create(req);
                            return complete(StatusCodes.CREATED);
                        })
                    )
                ),
                // GET /users/{id} — get one user (dynamic segment)
                path(segment(), id ->
                    get(() ->
                        complete(userService.findById(id))
                    )
                ),
                // PUT /users/{id} — update a user
                path(segment(), id ->
                    put(() ->
                        entity(as(UpdateUserRequest.class), req -> {
                            userService.update(id, req);
                            return complete(StatusCodes.OK);
                        })
                    )
                ),
                // DELETE /users/{id} — delete a user
                path(segment(), id ->
                    delete(() -> {
                        userService.delete(id);
                        return complete(StatusCodes.NO_CONTENT);
                    })
                )
            )
        );
    }

    // -------------------------------------------------------------------------
    // Order routes: request parameter extraction
    // -------------------------------------------------------------------------
    private Route orderRoutes() {
        return pathPrefix("orders", () ->
            concat(
                // GET /orders?status=<status>&page=<page>
                pathEnd(() ->
                    get(() ->
                        parameter("status", status ->
                            parameterOptional("page", pageOpt ->
                                complete(orderService.list(status, pageOpt.orElse("1")))
                            )
                        )
                    )
                ),
                // POST /orders
                pathEnd(() ->
                    post(() ->
                        entity(as(CreateOrderRequest.class), req -> {
                            orderService.create(req);
                            return complete(StatusCodes.CREATED);
                        })
                    )
                )
            )
        );
    }

    // -------------------------------------------------------------------------
    // Admin routes: auth directives (authenticateBasic, authorize)
    // -------------------------------------------------------------------------
    private Route adminRoutes() {
        return pathPrefix("admin", () ->
            // Basic auth guard over the entire /admin prefix
            authenticateBasic("admin-realm", credentials -> {
                if (credentials.verify("admin", "s3cr3t")) {
                    return Optional.of("admin");
                }
                return Optional.empty();
            }, user ->
                // Role-based authorization
                authorize(user.equals("admin"), () ->
                    concat(
                        // GET /admin/users
                        path("users", () ->
                            get(() ->
                                complete(userService.list())
                            )
                        ),
                        // POST /admin/config
                        path("config", () ->
                            post(() ->
                                entity(as(AdminConfigRequest.class), req -> {
                                    applyConfig(req);
                                    return complete(StatusCodes.OK);
                                })
                            )
                        )
                    )
                )
            )
        );
    }

    // -------------------------------------------------------------------------
    // OAuth2-protected route: authenticateOAuth2
    // -------------------------------------------------------------------------
    private Route protectedRoutes() {
        return path("protected", () ->
            authenticateOAuth2("my-realm", credentials ->
                Optional.of(credentials.token()), token ->
                get(() ->
                    complete("Access granted for token: " + token)
                )
            )
        );
    }

    // -------------------------------------------------------------------------
    // Header-based auth: headerValueByName("Authorization")
    // -------------------------------------------------------------------------
    private Route apiRoutes() {
        return pathPrefix("api", () ->
            headerValueByName("Authorization", token ->
                path("data", () ->
                    get(() -> complete("Authorized: " + token))
                )
            )
        );
    }

    private void applyConfig(AdminConfigRequest req) { /* no-op */ }

    // -------------------------------------------------------------------------
    // DTO types (inner classes for fixture self-containment)
    // -------------------------------------------------------------------------
    public static class CreateUserRequest {
        public String name;
        public String email;
    }

    public static class UpdateUserRequest {
        public String name;
        public String email;
    }

    public static class CreateOrderRequest {
        public String userId;
        public String productId;
        public int quantity;
    }

    public static class AdminConfigRequest {
        public String key;
        public String value;
    }

    // -------------------------------------------------------------------------
    // Stub services
    // -------------------------------------------------------------------------
    interface UserService {
        Object list();
        void create(CreateUserRequest req);
        Object findById(String id);
        void update(String id, UpdateUserRequest req);
        void delete(String id);
    }

    interface OrderService {
        Object list(String status, String page);
        void create(CreateOrderRequest req);
    }
}
