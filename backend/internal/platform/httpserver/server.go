// Package httpserver owns the Gin delivery boundary and HTTP process lifecycle.
package httpserver

import (
	"context"
	"errors"
	"net/http"

	"github.com/GulovM/PharmacyCRM/backend/internal/platform/config"
	"github.com/GulovM/PharmacyCRM/backend/internal/platform/logging"
	"github.com/GulovM/PharmacyCRM/backend/internal/shared/httpx"
	"github.com/gin-gonic/gin"
)

// Server wraps the explicit net/http server required for production operation.
type Server struct {
	Router    *gin.Engine
	server    *http.Server
	config    config.HTTPConfig
	readiness *Readiness
}

// New creates the router and delegates route and middleware composition to
// their focused components.
func New(httpConfig config.HTTPConfig, proxyCORS config.ProxyCORSConfig, logger *logging.Logger, readiness *Readiness) (*Server, error) {
	router := gin.New()
	if proxyCORS.TrustForwardedHeaders {
		if err := router.SetTrustedProxies([]string(proxyCORS.TrustedProxyCIDRs)); err != nil {
			return nil, err
		}
	} else if err := router.SetTrustedProxies(nil); err != nil {
		return nil, err
	}
	responder := httpx.NewResponder(logger)
	router.Use(MiddlewareChain(httpConfig, proxyCORS, logger, responder)...)
	server := &Server{
		Router: router, config: httpConfig, readiness: readiness,
		server: &http.Server{Addr: httpConfig.Address, Handler: router, ReadHeaderTimeout: httpConfig.ReadHeaderTimeout, ReadTimeout: httpConfig.ReadTimeout, WriteTimeout: httpConfig.WriteTimeout, IdleTimeout: httpConfig.IdleTimeout, MaxHeaderBytes: httpConfig.MaxHeaderBytes},
	}
	registerOperationalRoutes(router, responder, readiness)
	return server, nil
}

func (s *Server) ListenAndServe() error {
	err := s.server.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *Server) Shutdown(parent context.Context) error {
	s.readiness.SetDraining()
	ctx, cancel := context.WithTimeout(parent, s.config.ShutdownTimeout)
	defer cancel()
	return s.server.Shutdown(ctx)
}
