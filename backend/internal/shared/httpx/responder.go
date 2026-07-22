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
	c.JSON(response.status, errorEnvelope{Success: false, Error: publicError{Code: response.code, Message: response.message, Details: response.details}, Meta: Meta{RequestID: requestID(c)}})
}

type response struct {
	status               int
	code, message, level string
	details              []Detail
}

func classify(err error) response {
	var typed *apperror.Typed
	if errors.As(err, &typed) {
		response := classify(typed.Category)
		if typed.Code != "" {
			response.code = typed.Code
		}
		if errors.Is(typed.Category, apperror.ErrInvalidArgument) {
			response.details = make([]Detail, 0, len(typed.Details))
			for _, detail := range typed.Details {
				response.details = append(response.details, Detail{Field: detail.Field, Code: detail.Code, Message: detail.Message})
			}
		}
		return response
	}
	switch {
	case errors.Is(err, apperror.ErrInvalidArgument):
		return response{status: http.StatusBadRequest, code: "INVALID_ARGUMENT", message: "request is invalid", level: "warn"}
	case errors.Is(err, apperror.ErrUnauthenticated):
		return response{status: http.StatusUnauthorized, code: "UNAUTHENTICATED", message: "authentication is required", level: "info"}
	case errors.Is(err, apperror.ErrForbidden):
		return response{status: http.StatusForbidden, code: "FORBIDDEN", message: "operation is forbidden", level: "warn"}
	case errors.Is(err, apperror.ErrNotFound):
		return response{status: http.StatusNotFound, code: "NOT_FOUND", message: "resource not found", level: "info"}
	case errors.Is(err, apperror.ErrConflict):
		return response{status: http.StatusConflict, code: "CONFLICT", message: "operation conflicts with current state", level: "warn"}
	case errors.Is(err, apperror.ErrBusinessRule):
		return response{status: http.StatusUnprocessableEntity, code: "BUSINESS_RULE_VIOLATION", message: "operation violates a business rule", level: "info"}
	case errors.Is(err, apperror.ErrUnavailable):
		return response{status: http.StatusServiceUnavailable, code: "SERVICE_UNAVAILABLE", message: "service is temporarily unavailable", level: "error"}
	default:
		return response{status: http.StatusInternalServerError, code: "INTERNAL_ERROR", message: "internal server error", level: "error"}
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
