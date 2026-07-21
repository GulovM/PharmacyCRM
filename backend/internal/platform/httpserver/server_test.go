package httpserver

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/GulovM/PharmacyCRM/backend/internal/platform/config"
	"github.com/GulovM/PharmacyCRM/backend/internal/platform/logging"
	"github.com/gin-gonic/gin"
)

func testServer(t *testing.T) *Server {
	t.Helper()
	gin.SetMode(gin.TestMode)
	logger, err := logging.New(config.LoggingConfig{Level: "error", Format: "json", FilePath: t.TempDir() + "/app.log", MaxSizeMB: 1, MaxBackups: 1, MaxAgeDays: 1}, config.AppConfig{ServiceName: "test", Environment: "test", Version: "test", CommitSHA: "test", WorkerProtocol: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logger.Close() })
	server, err := New(config.HTTPConfig{Address: "127.0.0.1:0", ReadHeaderTimeout: time.Second, ReadTimeout: time.Second, WriteTimeout: time.Second, IdleTimeout: time.Second, ShutdownTimeout: time.Second, MaxHeaderBytes: 1024, MaxBodyBytes: 8}, config.ProxyCORSConfig{AllowedOrigins: config.CSV{"https://app.example"}, AllowCredentials: true}, logger)
	if err != nil {
		t.Fatal(err)
	}
	return server
}

func TestServerConfiguresExplicitHTTPServerAndMiddleware(t *testing.T) {
	server := testServer(t)
	server.Router.GET("/test", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	request := httptest.NewRequest(http.MethodGet, "/test", nil)
	request.Header.Set("Origin", "https://app.example")
	response := httptest.NewRecorder()
	server.Router.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d", response.Code)
	}
	if response.Header().Get(requestIDHeader) == "" {
		t.Fatal("request id missing")
	}
	if response.Header().Get("Access-Control-Allow-Origin") != "https://app.example" {
		t.Fatal("cors origin missing")
	}
	if response.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("security header missing")
	}
}

func TestServerLimitsRequestBody(t *testing.T) {
	server := testServer(t)
	server.Router.POST("/test", func(c *gin.Context) {
		_, err := c.Request.Body.Read(make([]byte, 16))
		if err != nil {
			c.Status(http.StatusRequestEntityTooLarge)
			return
		}
		c.Status(http.StatusNoContent)
	})
	response := httptest.NewRecorder()
	server.Router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/test", bytes.NewBufferString("too-large-body")))
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d", response.Code)
	}
}
