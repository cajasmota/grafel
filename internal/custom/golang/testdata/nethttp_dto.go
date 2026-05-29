package testdata

import (
	"encoding/json"
	"net/http"
)

// NetHTTPSignupReq is the request DTO decoded from the raw request body via the
// idiomatic stdlib json.NewDecoder(r.Body).Decode pattern.
type NetHTTPSignupReq struct {
	Username string `json:"username" validate:"required"`
	Email    string `json:"email" validate:"required,email"`
}

// NetHTTPSignupResp is the response DTO encoded via json.NewEncoder(w).Encode.
type NetHTTPSignupResp struct {
	ID     int    `json:"id"`
	Status string `json:"status"`
}

func nethttpSignup(w http.ResponseWriter, r *http.Request) {
	var req NetHTTPSignupReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	resp := NetHTTPSignupResp{ID: 1, Status: "created"}
	json.NewEncoder(w).Encode(resp)
}

func setupNetHTTP() {
	mux := http.NewServeMux()
	mux.HandleFunc("/signup", nethttpSignup)
	_ = mux
}
