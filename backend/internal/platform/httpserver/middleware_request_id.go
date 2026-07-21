package httpserver

import (
	"crypto/rand"
	"encoding/hex"
	"strings"

	"github.com/gin-gonic/gin"
)

const requestIDHeader = "X-Request-ID"

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

func currentRequestID(c *gin.Context) string {
	value, _ := c.Get("request_id")
	id, _ := value.(string)
	return id
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
