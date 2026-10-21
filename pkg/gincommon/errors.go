package gincommon

import (
	"github.com/gin-gonic/gin"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/adapter/inbound/http/middleware"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/pkg/problem"
)

// Every error response the stack writes is an RFC 9457 problem
// (application/problem+json, see pkg/problem). Handlers report errors in one of two
// ways, and the global error handler in ObservabilityMiddlewares renders them:
//
//	// Deferred: record the error and return; the handler renders it.
//	if err != nil {
//		_ = c.Error(err)
//		return
//	}
//
//	// Immediate: render now (e.g. from a middleware that must stop the chain).
//	gincommon.AbortWithProblem(c, problem.NewNotFoundError("Tender 123 was not found."))
//
// Errors are classified by ClassifyError. A *problem.Error is sent as it is; any
// unclassified error is a 500 internal-error with no detail, so error text never
// reaches the client.

// AbortWithProblem classifies err (ClassifyError), writes it as a problem with the
// request's traceId, instance and verified tenantId (and the *problem.Error's
// extension members, see problem.Error.WithExtension), aborts the chain and logs a
// warn-level "problem response" entry. If the response was already written it only
// aborts. A nil err is a 500 internal-error.
func AbortWithProblem(c *gin.Context, err error) {
	if c.Writer.Written() {
		c.Abort()
		return
	}
	if err == nil {
		middleware.WriteProblem(c, nil)
		return
	}
	middleware.WriteProblem(c, ClassifyError(err))
}

// ClassifyError maps a handler error to a problem error: a *problem.Error as it is;
// validator.ValidationErrors (from c.ShouldBind*) 400 validation-failed with
// validationErrors; ErrForbidden 403; then problem.FromError (ErrUnsupported 501,
// context.DeadlineExceeded 503, *http.MaxBytesError 413, anything else 500).
//
// Malformed-JSON and empty-body errors from c.ShouldBind* are only recognised through
// BindingError, ShouldBindJSON or c.Error(err).SetType(gin.ErrorTypeBind); a bare
// *json.SyntaxError may be an internal decoding failure, which is a 500.
func ClassifyError(err error) *problem.Error {
	if err == nil {
		return nil
	}
	return middleware.ClassifyError(err, false)
}

// BindingError maps an error from c.ShouldBind* to a 400 validation-failed problem
// error: one validationErrors entry per invalid field (its json name and a fixed
// message such as "is required", never the value), or a fixed detail for an empty
// or malformed body. An *http.MaxBytesError is 413. A nil err returns nil.
func BindingError(err error) *problem.Error {
	return middleware.BindingError(err)
}

// ShouldBindJSON binds the JSON body into obj and returns nil or a BindingError:
//
//	var req CreateTender
//	if err := gincommon.ShouldBindJSON(c, &req); err != nil {
//		gincommon.AbortWithProblem(c, err)
//		return
//	}
func ShouldBindJSON(c *gin.Context, obj any) error {
	if err := c.ShouldBindJSON(obj); err != nil {
		return middleware.BindingError(err)
	}
	return nil
}

// ShouldBind binds the request (by Content-Type and method) into obj and returns
// nil or a BindingError.
func ShouldBind(c *gin.Context, obj any) error {
	if err := c.ShouldBind(obj); err != nil {
		return middleware.BindingError(err)
	}
	return nil
}
