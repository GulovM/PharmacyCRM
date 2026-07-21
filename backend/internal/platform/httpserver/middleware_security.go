package httpserver

import (
	"net/http"

	"github.com/GulovM/PharmacyCRM/backend/internal/platform/config"
	"github.com/gin-gonic/gin"
)

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
