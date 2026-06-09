# Orders

The OrderService coordinates checkout for the storefront.

## Placing an order

Call placeOrder to submit a new order. It internally uses validateOrder
to check the cart before persisting.

### Validation

validateOrder rejects empty carts. This section should mention nothing
common like type, data, file, or test.

## Refunds

Refunds are handled by a separate service not described here.
