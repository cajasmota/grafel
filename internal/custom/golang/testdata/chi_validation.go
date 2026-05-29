package fixtures

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
	"github.com/go-playground/validator/v10"
)

// OrderReq is a chi-bound DTO using validate: struct tags, decoded via render.
type OrderReq struct {
	SKU   string `json:"sku" validate:"required,alphanum"`
	Qty   int    `json:"qty" validate:"required,gt=0"`
	Notes string `json:"notes" validate:"max=200"`
}

func setupChi() {
	r := chi.NewRouter()
	validate := validator.New()

	r.Post("/orders", func(w http.ResponseWriter, req *http.Request) {
		var body OrderReq
		if err := render.DecodeJSON(req.Body, &body); err != nil {
			render.Status(req, http.StatusBadRequest)
			return
		}
		if err := validate.Struct(&body); err != nil {
			render.Status(req, http.StatusBadRequest)
			return
		}
		render.JSON(w, req, body)
	})
}
