package utils

import (
	"encoding/json"
	"net/http"
)

type JSONResponse struct {
	Status  string      `json:"status"`
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data,omitempty"`
}

func WriteJSONResponse(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	var response JSONResponse
	if statusCode >= 200 && statusCode < 300 {
		response.Status = "success"
		response.Data = data
	} else {
		response.Status = "error"
		response.Message = data.(string) // Assuming data is a string for error messages
	}
	json.NewEncoder(w).Encode(response)
}
