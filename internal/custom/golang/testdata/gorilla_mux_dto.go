package testdata

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
)

// MuxCreateUserReq is the request DTO decoded from the request body.
type MuxCreateUserReq struct {
	Email string `json:"email" validate:"required,email"`
	Name  string `json:"name" validate:"required"`
}

// MuxUserResp is the response DTO encoded back to the client.
type MuxUserResp struct {
	ID    int    `json:"id"`
	Email string `json:"email"`
}

func muxCreateUser(w http.ResponseWriter, r *http.Request) {
	var req MuxCreateUserReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	resp := MuxUserResp{ID: 42, Email: req.Email}
	json.NewEncoder(w).Encode(resp)
}

func setupMux() {
	r := mux.NewRouter()
	r.HandleFunc("/users", muxCreateUser).Methods("POST")
	_ = r
}
