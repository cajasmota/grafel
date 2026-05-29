package com.example.microprofile;

import jakarta.enterprise.context.ApplicationScoped;
import jakarta.transaction.Transactional;
import jakarta.transaction.Transactional.TxType;

/**
 * MicroProfile JTA transaction fixture.
 *
 * MicroProfile uses the Jakarta Transactions (JTA) @Transactional annotation
 * (jakarta.transaction.Transactional) which is part of the MicroProfile
 * platform via Jakarta EE Core Profile.  This fixture exercises:
 *   - transaction_boundary_extraction: class-level and method-level boundaries
 *   - transaction_propagation: TxType positional form + named propagation=
 *   - transaction_rollback_rules: rollbackFor / noRollbackFor
 */
@ApplicationScoped
@Transactional
public class OrderService {

    public void createOrder(String item) {
        // default REQUIRED boundary inherited from class-level @Transactional
    }

    @Transactional(TxType.REQUIRES_NEW)
    public void auditOrder(String orderId) {
        // new independent transaction
    }

    @Transactional(value = TxType.MANDATORY, rollbackOn = OrderException.class)
    public void confirmPayment(String paymentId) {
        // must be called inside an existing transaction; rolls back on OrderException
    }

    @Transactional(value = TxType.SUPPORTS, dontRollbackOn = IllegalArgumentException.class)
    public String queryOrderStatus(String orderId) {
        // participates if a transaction exists, otherwise non-transactional
        return "PENDING";
    }

    @Transactional(TxType.NOT_SUPPORTED)
    public void sendNotification(String message) {
        // suspends any active transaction while running
    }

    @Transactional(TxType.NEVER)
    public void healthCheck() {
        // must NOT be called inside a transaction
    }
}
