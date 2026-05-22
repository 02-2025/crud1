package middleware

import (
	"service/internal/response"
	"service/internal/logger"
	"net/http"
	"time"
	"fmt"
)

// middleware для логирования времени получения запроса и отправки ответа, метода и затраченного времени на обработку запроса
func LoggerMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		date := time.Now()
		logger.Info(r, "Request received")
		next(w, r)
		logger.Info(r, fmt.Sprintf("Response sent — %dms", time.Since(date).Milliseconds()))
	}
}

// middleware для перехвата фатальной ошибки, и остановки последовательности завершения программы
func RecoveryMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				logger.Error(r, fmt.Errorf("Panic: %v", err))
				response.Send(w, response.Response{
					Success: false, Error: "Internal Server Error",
				}, http.StatusInternalServerError)
			}
		}()
		next(w, r)
	}
}

// middleware для проверки метода запроса, на соответствие с переданным в аргументе
func MethodValidationMiddleware(next http.HandlerFunc, method string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method {
			response.Send(w, response.Response{
				Success: false, Error: "Method " + r.Method + " not allowed, " + method + " expected",
			}, http.StatusMethodNotAllowed)
			logger.Info(r, "Method not allowed")
			return
		}
		next(w, r)
	}
}
