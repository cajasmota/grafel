package fixtures

import (
	"encoding/json"
	"net/http"

	"github.com/go-playground/validator/v10"
	"github.com/gorilla/mux"
)

// GorillaOrderReq is a DTO decoded from the request body and validated with
// go-playground validate: tags — gorilla/mux has no built-in binding, so
// validation is the conventional decode-then-validator.Struct pattern.
type GorillaOrderReq struct {
	SKU      string `json:"sku" validate:"required,alphanum"`
	Quantity int    `json:"quantity" validate:"required,gte=1"`
}

func newGorillaValidationRouter() *mux.Router {
	r := mux.NewRouter()
	validate := validator.New()

	r.HandleFunc("/orders", func(w http.ResponseWriter, req *http.Request) {
		var body GorillaOrderReq
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if err := validate.Struct(&body); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusCreated)
	}).Methods("POST")
	return r
}
