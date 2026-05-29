package fixtures

import (
	"encoding/json"
	"net/http"

	"github.com/go-playground/validator/v10"
	"github.com/gorilla/mux"
)

// MuxCreateUserReq is a gorilla-mux-handled DTO validated via a
// validator.New() struct-tag check after a std-lib JSON decode.
type MuxCreateUserReq struct {
	Name  string `json:"name" validate:"required,min=2"`
	Email string `json:"email" validate:"required,email"`
}

func setupMuxVal() {
	r := mux.NewRouter()
	validate := validator.New()
	r.HandleFunc("/users", func(w http.ResponseWriter, req *http.Request) {
		var dto MuxCreateUserReq
		if err := json.NewDecoder(req.Body).Decode(&dto); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if err := validate.Struct(&dto); err != nil {
			http.Error(w, err.Error(), 422)
			return
		}
		w.WriteHeader(201)
	}).Methods("POST")
}
