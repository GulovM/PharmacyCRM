// Package httpserver owns the Gin delivery boundary and HTTP process lifecycle.
package httpserver

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/GulovM/PharmacyCRM/backend/internal/platform/config"
	"github.com/GulovM/PharmacyCRM/backend/internal/platform/logging"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

const requestIDHeader = "X-Request-ID"

// Server wraps the explicit net/http server required for production operation.
type Server struct {
	Router *gin.Engine
	server *http.Server
	config config.HTTPConfig
	logger *logging.Logger
}

// New builds a Gin router with explicitly ordered middleware and never relies
// on gin.Default's implicit logger or recovery middleware.
func New(httpConfig config.HTTPConfig, proxyCORS config.ProxyCORSConfig, logger *logging.Logger) (*Server, error) {
	router := gin.New()
	if proxyCORS.TrustForwardedHeaders {
		if err := router.SetTrustedProxies([]string(proxyCORS.TrustedProxyCIDRs)); err != nil {
			return nil, err
		}
	} else if err := router.SetTrustedProxies(nil); err != nil {
		return nil, err
	}

	// This order is normative. Auth and route-policy middleware are intentional
	// placeholders until E3 introduces their concrete policies.
	router.Use(
		requestID(),
		recovery(logger),
		accessLog(logger),
		tracingMetrics(),
		bodyLimit(httpConfig.MaxBodyBytes),
		corsAndSecurity(proxyCORS),
		authenticationParsing(),
		routePolicyAndRateLimit(),
	)

	return &Server{
		Router: router,
		config: httpConfig,
		logger: logger,
		server: &http.Server{
			Addr: httpConfig.Address, Handler: router,
			ReadHeaderTimeout: httpConfig.ReadHeaderTimeout,
			ReadTimeout:       httpConfig.ReadTimeout,
			WriteTimeout:      httpConfig.WriteTimeout,
			IdleTimeout:       httpConfig.IdleTimeout,
			MaxHeaderBytes:    httpConfig.MaxHeaderBytes,
		},
	}, nil
}

func (s *Server) ListenAndServe() error {
	err := s.server.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *Server) Shutdown(parent context.Context) error {
	ctx, cancel := context.WithTimeout(parent, s.config.ShutdownTimeout)
	defer cancel()
	return s.server.Shutdown(ctx)
}

func requestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(requestIDHeader)
		if !validRequestID(id) {
			id = newRequestID()
		}
		c.Set("request_id", id)
		c.Header(requestIDHeader, id)
		c.Next()
	}
}

func recovery(logger *logging.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if recovered := recover(); recovered != nil {
				logger.Error("http.panic", zap.String("request_id", currentRequestID(c)), zap.ByteString("stack", debug.Stack()))
				c.AbortWithStatus(http.StatusInternalServerError)
			}
		}()
		c.Next()
	}
}

func accessLog(logger *logging.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		started := time.Now()
		c.Next()
		logger.Info("http.request.completed",
			zap.String("request_id", currentRequestID(c)), zap.String("method", c.Request.Method),
			zap.String("route", c.FullPath()), zap.Int("status", c.Writer.Status()),
			zap.Int("response_bytes", c.Writer.Size()), zap.Duration("duration", time.Since(started)),
		)
	}
}

func tracingMetrics() gin.HandlerFunc          { return func(c *gin.Context) { c.Next() } }
func authenticationParsing() gin.HandlerFunc   { return func(c *gin.Context) { c.Next() } }
func routePolicyAndRateLimit() gin.HandlerFunc { return func(c *gin.Context) { c.Next() } }

func bodyLimit(limit int64) gin.HandlerFunc {
	return func(c *gin.Context) { c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit); c.Next() }
}

func corsAndSecurity(config config.ProxyCORSConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("Referrer-Policy", "no-referrer")
		origin := c.GetHeader("Origin")
		if origin != "" && allowedOrigin(origin, config.AllowedOrigins) {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Vary", "Origin")
			if config.AllowCredentials {
				c.Header("Access-Control-Allow-Credentials", "true")
			}
			c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type, X-CSRF-Protection, X-Request-ID")
		}
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

func allowedOrigin(origin string, origins config.CSV) bool {
	for _, allowed := range origins {
		if origin == allowed {
			return true
		}
	}
	return false
}
func currentRequestID(c *gin.Context) string {
	if value, ok := c.Get("request_id"); ok {
		return value.(string)
	}
	return ""
}
func validRequestID(id string) bool {
	return len(id) > 0 && len(id) <= 128 && !strings.ContainsAny(id, "\r\n")
}
func newRequestID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "unavailable"
	}
	return hex.EncodeToString(raw[:])
}
