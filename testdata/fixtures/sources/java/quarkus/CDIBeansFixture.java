package io.example.quarkus;

import jakarta.enterprise.context.ApplicationScoped;
import jakarta.enterprise.context.RequestScoped;
import jakarta.inject.Inject;

/**
 * Fixture for Quarkus CDI DI extraction tests.
 * Proves: di_binding_extraction, di_injection_point, di_scope_resolution.
 *
 * Expected extractions:
 *   - SCOPE.Service for OrderService with cdi_scope=ApplicationScoped
 *   - SCOPE.Service for OrderController with cdi_scope=RequestScoped
 *   - DEPENDS_ON from OrderController -> OrderService  (injection_kind=cdi_inject)
 *   - DEPENDS_ON from OrderService -> OrderRepository  (injection_kind=cdi_constructor)
 */

@ApplicationScoped
public class OrderService {

    private final OrderRepository repository;

    @Inject
    public OrderService(OrderRepository repository) {
        this.repository = repository;
    }
}

@RequestScoped
public class OrderController {

    @Inject
    private OrderService orderService;
}
