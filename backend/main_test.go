package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestCorsAllowsLoopbackPortsDuringDevelopment(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("CORS_ALLOWED_ORIGINS", "")
	router := gin.New()
	router.Use(corsMiddleware())

	request := httptest.NewRequest(http.MethodOptions, "/api/auth/login", nil)
	request.Header.Set("Origin", "http://localhost:5174")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("got status %d, want %d", response.Code, http.StatusNoContent)
	}
	if got := response.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:5174" {
		t.Fatalf("got allowed origin %q", got)
	}
}

func TestCorsRejectsUnlistedProductionOrigins(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://travo.example")
	router := gin.New()
	router.Use(corsMiddleware())

	request := httptest.NewRequest(http.MethodOptions, "/api/auth/login", nil)
	request.Header.Set("Origin", "http://localhost:5174")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("got status %d, want %d", response.Code, http.StatusForbidden)
	}
}
