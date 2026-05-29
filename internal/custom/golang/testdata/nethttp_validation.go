package fixtures

import (
	"encoding/json"
	"net/http"

	"github.com/go-playground/validator/v10"
)

// NetHTTPSignupReq is a net/http-handled DTO validated via a validator.New()
// struct-tag check after a std-lib JSON decode.
type NetHTTPSignupReq struct {
	Username string `json:"username" validate:"required,alphanum"`
	Password string `json:"password" validate:"required,min=8"`
}

func setupNetHTTPVal() {
	mux := http.NewServeMux()
	validate := validator.New()
	mux.HandleFunc("/signup", func(w http.ResponseWriter, req *http.Request) {
		var dto NetHTTPSignupReq
		if err := json.NewDecoder(req.Body).Decode(&dto); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if err := validate.Struct(&dto); err != nil {
			http.Error(w, err.Error(), 422)
			return
		}
		w.WriteHeader(201)
	})
}
