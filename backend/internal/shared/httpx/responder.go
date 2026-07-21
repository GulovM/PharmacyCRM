// Package httpx provides shared HTTP transport helpers.
package httpx

import (
	"errors"
	"net/http"

	"github.com/GulovM/PharmacyCRM/backend/internal/platform/logging"
	"github.com/GulovM/PharmacyCRM/backend/internal/shared/apperror"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type Meta struct {
	RequestID string `json:"request_id"`
}
type successEnvelope struct {
	Success bool `json:"success"`
	Data    any  `json:"data"`
	Meta    Meta `json:"meta"`
}
type errorEnvelope struct {
	Success bool        `json:"success"`
	Error   publicError `json:"error"`
	Meta    Meta        `json:"meta"`
}
type publicError struct {
	Code    string   `json:"code"`
	Message string   `json:"message"`
	Details []Detail `json:"details,omitempty"`
}
type Detail struct {
	Field   string `json:"field,omitempty"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Responder is the only component that writes API success/error envelopes.
type Responder struct{ logger *logging.Logger }

func NewResponder(logger *logging.Logger) *Responder { return &Responder{logger: logger} }

func (r *Responder) JSON(c *gin.Context, status int, data any) {
	if c.Writer.Written() {
		return
	}
	c.JSON(status, successEnvelope{Success: true, Data: data, Meta: Meta{RequestID: requestID(c)}})
}
func (r *Responder) NoContent(c *gin.Context) {
	if !c.Writer.Written() {
		c.Status(http.StatusNoContent)
		c.Writer.WriteHeaderNow()
	}
}
func (r *Responder) Error(c *gin.Context, err error, operation string) {
	r.write(c, classify(err), operation)
}
func (r *Responder) Panic(c *gin.Context, operation string) {
	r.write(c, response{status: http.StatusInternalServerError, code: "INTERNAL_ERROR", message: "internal server error", level: "error"}, operation)
}

func (r *Responder) write(c *gin.Context, response response, operation string) {
	if c.Writer.Written() {
		r.logger.Error("http.response_already_written", zap.String("request_id", requestID(c)), zap.String("operation", operation))
		return
	}
	log := r.logger.With(zap.String("request_id", requestID(c)), zap.String("operation", operation), zap.String("error_code", response.code))
	switch response.level {
	case "error":
		log.Error("http.request.failed")
	case "warn":
		log.Warn("http.request.failed")
	default:
		log.Info("http.request.failed")
	}
	c.JSON(response.status, errorEnvelope{Success: false, Error: publicError{Code: response.code, Message: response.message}, Meta: Meta{RequestID: requestID(c)}})
}

type response struct {
	status               int
	code, message, level string
}

func classify(err error) response {
	switch {
	case errors.Is(err, apperror.ErrInvalidArgument):
		return response{http.StatusBadRequest, "INVALID_ARGUMENT", "request is invalid", "warn"}
	case errors.Is(err, apperror.ErrUnauthenticated):
		return response{http.StatusUnauthorized, "UNAUTHENTICATED", "authentication is required", "info"}
	case errors.Is(err, apperror.ErrForbidden):
		return response{http.StatusForbidden, "FORBIDDEN", "operation is forbidden", "warn"}
	case errors.Is(err, apperror.ErrNotFound):
		return response{http.StatusNotFound, "NOT_FOUND", "resource not found", "info"}
	case errors.Is(err, apperror.ErrConflict):
		return response{http.StatusConflict, "CONFLICT", "operation conflicts with current state", "warn"}
	case errors.Is(err, apperror.ErrBusinessRule):
		return response{http.StatusUnprocessableEntity, "BUSINESS_RULE_VIOLATION", "operation violates a business rule", "info"}
	case errors.Is(err, apperror.ErrUnavailable):
		return response{http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "service is temporarily unavailable", "error"}
	default:
		return response{http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error", "error"}
	}
}
func requestID(c *gin.Context) string {
	value, ok := c.Get("request_id")
	if !ok {
		return ""
	}
	id, _ := value.(string)
	return id
}
