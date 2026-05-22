package main

import (
	"service/internal/middleware"
	"service/internal/handlers"
	"service/internal/logger"
	"net/http"
)



func main() {
	logger.Init()
	mux := http.NewServeMux()
	mux.HandleFunc("/getinfo", middleware.MethodValidationMiddleware(handlers.GetInfoHandler, http.MethodGet))
	mux.HandleFunc("/getstats", middleware.MethodValidationMiddleware(handlers.GetFileStatsHandler, http.MethodGet))
	mux.HandleFunc("/getlist", middleware.MethodValidationMiddleware(handlers.GetListHandler, http.MethodGet))
	mux.HandleFunc("/read", middleware.MethodValidationMiddleware(handlers.ReadFileHandler, http.MethodGet))
	mux.HandleFunc("/search", middleware.MethodValidationMiddleware(handlers.SearchByNameHandler, http.MethodGet))
	mux.HandleFunc("/write", middleware.MethodValidationMiddleware(handlers.WriteHandler, http.MethodPatch))
	mux.HandleFunc("/createfile", middleware.MethodValidationMiddleware(handlers.CreateFileHandler, http.MethodPost))
	mux.HandleFunc("/createdir", middleware.MethodValidationMiddleware(handlers.CreateDirHandler, http.MethodPost))
	mux.HandleFunc("/copy", middleware.MethodValidationMiddleware(handlers.CopyHandler, http.MethodPost))
	mux.HandleFunc("/move", middleware.MethodValidationMiddleware(handlers.MoveHandler, http.MethodPost))
	mux.HandleFunc("/delete", middleware.MethodValidationMiddleware(handlers.DeleteHandler, http.MethodDelete))
	handler := middleware.LoggerMiddleware(middleware.RecoveryMiddleware(mux.ServeHTTP))
	logger.Fatal(http.ListenAndServe(":10000", handler))

}
