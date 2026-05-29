package fixtures

import (
	"encoding/json"
	"net/http"

	"github.com/go-playground/validator/v10"
)

// NetHTTPCreateUserReq is a DTO decoded from the request body and validated
// with go-playground validate: tags — the stdlib net/http router has no
// built-in binding, so validation is the conventional decode-then-validator
// pattern.
type NetHTTPCreateUserReq struct {
	Name  string `json:"name" validate:"required,min=2"`
	Email string `json:"email" validate:"required,email"`
}

func newNetHTTPValidationServer() *http.ServeMux {
	mux := http.NewServeMux()
	validate := validator.New()

	mux.HandleFunc("POST /users", func(w http.ResponseWriter, r *http.Request) {
		var req NetHTTPCreateUserReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if err := validate.Struct(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusCreated)
	})
	return mux
}
