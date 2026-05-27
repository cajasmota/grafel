// Chi handler definitions. These lines should own the http_endpoint
// synthetic entities post-#2678.
package chifiber

import (
	"net/http"
)

// listOrders handles GET /orders.
func listOrders(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(200)
	_, _ = w.Write([]byte(`{"orders":[]}`))
}

// createOrder handles POST /orders.
func createOrder(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(201)
	_, _ = w.Write([]byte(`{"id":1}`))
}
