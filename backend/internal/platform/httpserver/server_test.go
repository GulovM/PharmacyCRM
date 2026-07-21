package httpserver

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
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
	readiness := NewReadiness(pingOK{}, func(context.Context) error { return nil }, func(context.Context) error { return nil }, func(context.Context) error { return nil })
	readiness.MarkStartupComplete()
	server, err := New(config.HTTPConfig{Address: "127.0.0.1:0", ReadHeaderTimeout: time.Second, ReadTimeout: time.Second, WriteTimeout: time.Second, IdleTimeout: time.Second, ShutdownTimeout: time.Second, MaxHeaderBytes: 1024, MaxBodyBytes: 8}, config.ProxyCORSConfig{AllowedOrigins: config.CSV{"https://app.example"}, AllowCredentials: true}, logger, readiness)
	if err != nil {
		t.Fatal(err)
	}
	return server
}

func TestOperationalEndpointsSeparateLivenessAndReadiness(t *testing.T) {
	server := testServer(t)
	response := httptest.NewRecorder()
	server.Router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("health status=%d", response.Code)
	}
	server.readiness.SetDraining()
	response = httptest.NewRecorder()
	server.Router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if response.Code != http.StatusServiceUnavailable || strings.Contains(response.Body.String(), "database") {
		t.Fatalf("unsafe readiness response: %d %s", response.Code, response.Body.String())
	}
}

type pingOK struct{}

func (pingOK) Ping(context.Context) error { return nil }

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

func TestReadinessRequiresStartupAndEveryCriticalCheck(t *testing.T) {
	blocked := errors.New("blocked")
	readiness := NewReadiness(pingOK{}, func(context.Context) error { return blocked }, func(context.Context) error { return nil }, func(context.Context) error { return nil })
	if err := readiness.Ready(context.Background()); err == nil {
		t.Fatal("readiness passed before startup completion")
	}
	readiness.MarkStartupComplete()
	if err := readiness.Ready(context.Background()); !errors.Is(err, blocked) {
		t.Fatalf("readiness error = %v", err)
	}
}

func TestShutdownDrainsAndWaitsForInFlightRequest(t *testing.T) {
	server := testServer(t)
	started := make(chan struct{})
	release := make(chan struct{})
	server.Router.GET("/slow", func(c *gin.Context) { close(started); <-release; c.Status(http.StatusNoContent) })
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() { _ = server.server.Serve(listener) }()
	requestDone := make(chan error, 1)
	go func() {
		response, err := http.Get("http://" + listener.Addr().String() + "/slow")
		if err == nil {
			response.Body.Close()
		}
		requestDone <- err
	}()
	<-started
	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- server.Shutdown(context.Background()) }()
	for !server.readiness.draining.Load() {
		runtime.Gosched()
	}
	if err := server.readiness.Ready(context.Background()); err == nil {
		t.Fatal("server remained ready while draining")
	}
	select {
	case err := <-shutdownDone:
		t.Fatalf("shutdown returned before in-flight request: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	if err := <-requestDone; err != nil {
		t.Fatal(err)
	}
	if err := <-shutdownDone; err != nil {
		t.Fatal(err)
	}
}

func TestShutdownIsBoundedByConfiguredTimeout(t *testing.T) {
	server := testServer(t)
	server.config.ShutdownTimeout = 10 * time.Millisecond
	started := make(chan struct{})
	release := make(chan struct{})
	server.Router.GET("/slow-timeout", func(c *gin.Context) { close(started); <-release })
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() { _ = server.server.Serve(listener) }()
	go func() { _, _ = http.Get("http://" + listener.Addr().String() + "/slow-timeout") }()
	<-started
	if err := server.Shutdown(context.Background()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("shutdown error = %v", err)
	}
	close(release)
}
