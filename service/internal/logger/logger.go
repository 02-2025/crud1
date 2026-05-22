package logger

import (
	"log"
	"net/http"
	"os"
)

func Init() {
	log.SetFlags(log.LstdFlags)
	log.SetOutput(os.Stdout)
	log.Println("Logger initialized")
}

func Info(r *http.Request, message string) {
	log.Printf("[INFO] %s %s | %s", r.Method, r.URL.Path, message)
}

func Error(r *http.Request, err error) {
	log.Printf("[ERROR] %s %s | %v", r.Method, r.URL.Path, err)
}

func Fatal(err error) {
	log.Fatalf("[FATAL] %v", err)
}