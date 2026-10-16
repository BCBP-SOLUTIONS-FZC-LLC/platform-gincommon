// Package customvalidator_test runs in its own process so the problem middleware's
// one-time json field-name registration sees a binding.Validator the service replaced.
package customvalidator_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/adapter/inbound/http/middleware"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/pkg/gincommon"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/pkg/problem"
)

var errCustom = errors.New("custom: title must not be empty")

// customValidator is a service-supplied StructValidator whose Engine is not a
// *validator.Validate.
type customValidator struct{ calls int }

func (v *customValidator) ValidateStruct(obj any) error {
	v.calls++
	if r, ok := obj.(*struct {
		Title string `json:"title"`
	}); ok && r.Title == "" {
		return errCustom
	}
	return nil
}
func (v *customValidator) Engine() any { return "not-a-validator" }

func TestProblemMiddleware_LeavesReplacedValidatorAlone(t *testing.T) {
	custom := &customValidator{}
	binding.Validator = custom
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(middleware.ProblemMiddleware(middleware.ProblemSettings{})) // must not panic on the foreign engine
	r.POST("/x", func(c *gin.Context) {
		var req struct {
			Title string `json:"title"`
		}
		if err := gincommon.ShouldBindJSON(c, &req); err != nil {
			gincommon.AbortWithProblem(c, err)
			return
		}
		c.Status(http.StatusCreated)
	})

	if binding.Validator != custom {
		t.Fatal("the service's validator was replaced")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{"title":""}`)))
	if custom.calls != 1 {
		t.Fatalf("custom validator calls = %d, want 1", custom.calls)
	}
	// An unknown validator error is still a 400 validation-failed, with a fixed
	// detail: the validator's own message is never sent.
	if w.Code != http.StatusBadRequest || w.Header().Get("Content-Type") != problem.ContentType ||
		strings.Contains(w.Body.String(), "custom:") || !strings.Contains(w.Body.String(), "/platform/validation-failed") {
		t.Fatalf("response = %d %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{"title":"ok"}`)))
	if w.Code != http.StatusCreated {
		t.Fatalf("valid body = %d %s", w.Code, w.Body.String())
	}
}
