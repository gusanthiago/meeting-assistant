package response

import (
	"encoding/json"
	"net/http"
)

type WebResponse struct {
	Code   int    `json:"code"`
	Status string `json:"status"`
	Data   any    `json:"data,omitempty"`
	Error  string `json:"error,omitempty"`
}

// JSON writes the given data inside the response envelope with the given code.
func JSON(w http.ResponseWriter, code int, data any) {
	write(w, code, WebResponse{Code: code, Status: http.StatusText(code), Data: data})
}

// Fail writes the given message inside the response envelope with the given code.
func Fail(w http.ResponseWriter, code int, message string) {
	write(w, code, WebResponse{Code: code, Status: http.StatusText(code), Error: message})
}

func write(w http.ResponseWriter, code int, body WebResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}
