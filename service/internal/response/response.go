package response

import (
	"encoding/json"
	"net/http"
)

// структура для отправки ответа на запрос через функцию sendResponse()
type Response struct {
	Success bool   `json:"success"`
	Content any    `json:"content,omitempty"`
	Error   string `json:"error,omitempty"`
}

// функция отправки json ответа на запрос
func Send(w http.ResponseWriter, data Response, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(data)
}