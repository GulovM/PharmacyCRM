package httpserver

import (
	"fmt"
	"net/http"

	"github.com/GulovM/PharmacyCRM/backend/internal/shared/apperror"
	"github.com/GulovM/PharmacyCRM/backend/internal/shared/httpx"
	"github.com/gin-gonic/gin"
)

func registerOperationalRoutes(router *gin.Engine, responder *httpx.Responder, readiness *Readiness) {
	router.GET("/healthz", func(c *gin.Context) { responder.JSON(c, http.StatusOK, gin.H{"status": "ok"}) })
	router.GET("/readyz", func(c *gin.Context) {
		if err := readiness.Ready(c.Request.Context()); err != nil {
			responder.Error(c, fmt.Errorf("readiness: %w", apperror.ErrUnavailable), "readiness")
			return
		}
		responder.JSON(c, http.StatusOK, gin.H{"status": "ready"})
	})
}
