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
	// keep '&' literal so signed URLs in responses survive copy-paste from raw output
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.Encode(response)
}

// CookieValues returns every non-empty value the request carries for the named cookie.
// A client can hold several cookies with the same name under different paths
// (stale ones from older versions, or empty ones left behind by a delete),
// and r.Cookie returns only the first, which may not be the valid one.
func CookieValues(r *http.Request, name string) []string {
	var values []string
	for _, c := range r.CookiesNamed(name) {
		if c.Value != "" {
			values = append(values, c.Value)
		}
	}
	return values
}
