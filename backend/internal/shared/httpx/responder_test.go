package httpx

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/GulovM/PharmacyCRM/backend/internal/platform/config"
	"github.com/GulovM/PharmacyCRM/backend/internal/platform/logging"
	"github.com/GulovM/PharmacyCRM/backend/internal/shared/apperror"
	"github.com/gin-gonic/gin"
)

func responder(t *testing.T) *Responder {
	t.Helper()
	logger, err := logging.New(config.LoggingConfig{Level: "error", Format: "json", FilePath: t.TempDir() + "/app.log", MaxSizeMB: 1, MaxBackups: 1, MaxAgeDays: 1}, config.AppConfig{ServiceName: "test", Environment: "test", WorkerProtocol: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logger.Close() })
	return NewResponder(logger)
}
func contextWithID() (*gin.Context, *httptest.ResponseRecorder) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set("request_id", "request-1")
	return c, recorder
}
func TestErrorMapsWrappedCategoryWithoutLeakingInternalText(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := responder(t)
	c, recorder := contextWithID()
	r.Error(c, fmt.Errorf("query failed: %w; password=secret", apperror.ErrConflict), "test")
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status=%d", recorder.Code)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "CONFLICT") || strings.Contains(body, "password") || !strings.Contains(body, "request-1") {
		t.Fatalf("unsafe or invalid body: %s", body)
	}
}
func TestErrorMapsUnknownErrorToSafeInternalResponse(t *testing.T) {
	r := responder(t)
	c, recorder := contextWithID()
	r.Error(c, errors.New("SQLSTATE 23505 password=secret"), "test")
	if recorder.Code != http.StatusInternalServerError || strings.Contains(recorder.Body.String(), "secret") {
		t.Fatalf("unsafe response: %d %s", recorder.Code, recorder.Body.String())
	}
}
func TestJSONAndNoContentUseContract(t *testing.T) {
	r := responder(t)
	c, recorder := contextWithID()
	r.JSON(c, http.StatusOK, gin.H{"id": "1"})
	if !strings.Contains(recorder.Body.String(), "\"success\":true") {
		t.Fatal("success envelope missing")
	}
	c, recorder = contextWithID()
	r.NoContent(c)
	if recorder.Code != http.StatusNoContent || recorder.Body.Len() != 0 {
		t.Fatal("invalid no-content response")
	}
}

func TestErrorMapsTypedValidationDetails(t *testing.T) {
	r := responder(t)
	c, recorder := contextWithID()
	r.Error(c, &apperror.Typed{Category: apperror.ErrInvalidArgument, Details: []apperror.Detail{{Field: "email", Code: "INVALID", Message: "email is invalid"}}}, "test")
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "email is invalid") {
		t.Fatalf("invalid typed response: %s", recorder.Body.String())
	}
}

func TestErrorMapsTypedStableCode(t *testing.T) {
	r := responder(t)
	c, recorder := contextWithID()
	r.Error(c, &apperror.Typed{Category: apperror.ErrConflict, Code: "IDEMPOTENCY_KEY_REUSED"}, "test")
	if recorder.Code != http.StatusConflict || !strings.Contains(recorder.Body.String(), "IDEMPOTENCY_KEY_REUSED") {
		t.Fatalf("invalid typed response: %s", recorder.Body.String())
	}
}

func FuzzResponderNeverLeaksUnknownError(f *testing.F) {
	f.Add("password=secret SQLSTATE 23505")
	f.Fuzz(func(t *testing.T, value string) {
		r := responder(t)
		c, recorder := contextWithID()
		internal := "secret:" + value
		r.Error(c, errors.New(internal), "fuzz")
		if recorder.Code != http.StatusInternalServerError || strings.Contains(recorder.Body.String(), internal) {
			t.Fatalf("unsafe response: %s", recorder.Body.String())
		}
	})
}
