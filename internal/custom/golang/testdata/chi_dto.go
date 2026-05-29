package testdata

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
)

// OrderReq is the request DTO bound by chi's render.Bind.
type OrderReq struct {
	SKU      string `json:"sku" validate:"required"`
	Quantity int    `json:"quantity" validate:"gte=1"`
}

// OrderResp is the response DTO serialised by render.JSON.
type OrderResp struct {
	OrderID string `json:"order_id"`
	Total   int    `json:"total"`
}

func createOrder(w http.ResponseWriter, r *http.Request) {
	var req OrderReq
	if err := render.Bind(r, &req); err != nil {
		return
	}
	resp := OrderResp{OrderID: "o-1", Total: 99}
	render.JSON(w, r, resp)
}

func setupChi() {
	mux := chi.NewRouter()
	mux.Post("/orders", createOrder)
	_ = mux
}
