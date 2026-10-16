// Package apierror renders API errors in a single, predictable shape inspired
// by Stripe:
//
//	{
//	  "error": {
//	    "type": "invalid_request_error",
//	    "code": "parameter_missing",
//	    "message": "The email parameter is required.",
//	    "param": "email"
//	  }
//	}
//
// "type" is a coarse category clients can branch on, "code" is a stable,
// machine-readable reason, "message" is human readable and may change, and
// "param" names the offending request parameter when there is one.
package apierror

import (
	"net/http"

	"cv-evaluator/internal/telemetry"

	"github.com/gin-gonic/gin"
)

// Error types. These are deliberately few and coarse.
const (
	// TypeInvalidRequest covers malformed requests, validation failures,
	// missing resources and conflicts with existing state.
	TypeInvalidRequest = "invalid_request_error"
	// TypeAuthentication means the request could not be authenticated.
	TypeAuthentication = "authentication_error"
	// TypeAPI is an unexpected failure on our side.
	TypeAPI = "api_error"
)

// Error codes shared across handlers. Handlers may define additional codes.
const (
	CodeInvalidJSON        = "invalid_json"
	CodeParameterMissing   = "parameter_missing"
	CodeParameterInvalid   = "parameter_invalid"
	CodeResourceMissing    = "resource_missing"
	CodeMissingCredentials = "missing_credentials"
	CodeInvalidCredentials = "invalid_credentials"
	CodeInvalidToken       = "invalid_token"
	CodeInvalidAPIKey      = "invalid_api_key"
	CodeEmailAlreadyUsed   = "email_already_used"
	CodePhoneAlreadyUsed   = "phone_already_used"
	CodeInternal           = "internal_error"
	CodeTimeout            = "timeout"
)

// Error is the body of the "error" object.
type Error struct {
	Type    string `json:"type"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message"`
	Param   string `json:"param,omitempty"`
}

// Error implements the error interface so an *Error can travel through Go code.
func (e *Error) Error() string { return e.Message }

// Response is the top-level JSON envelope.
type Response struct {
	Error Error `json:"error"`
}

// Write renders err with the given HTTP status.
func Write(c *gin.Context, status int, err Error) {
	c.JSON(status, Response{Error: err})
}

// Abort renders err with the given HTTP status and stops the handler chain.
func Abort(c *gin.Context, status int, err Error) {
	c.AbortWithStatusJSON(status, Response{Error: err})
}

// InvalidRequest builds a 400-class validation error.
func InvalidRequest(code, message, param string) Error {
	return Error{Type: TypeInvalidRequest, Code: code, Message: message, Param: param}
}

// Authentication builds a 401 error.
func Authentication(code, message string) Error {
	return Error{Type: TypeAuthentication, Code: code, Message: message}
}

// Internal builds a generic 500 error. The message never includes internal
// details; log those server side instead.
func Internal() Error {
	return Error{Type: TypeAPI, Code: CodeInternal, Message: "An unexpected error occurred. Please try again later."}
}

// BadRequest writes a 400 invalid_request_error.
func BadRequest(c *gin.Context, code, message, param string) {
	Write(c, http.StatusBadRequest, InvalidRequest(code, message, param))
}

// Unauthorized aborts with a 401 authentication_error and sets a
// WWW-Authenticate header as required by RFC 9110.
func Unauthorized(c *gin.Context, code, message string) {
	c.Header("WWW-Authenticate", `Bearer realm="cv-evaluator"`)
	Abort(c, http.StatusUnauthorized, Authentication(code, message))
}

// NotFound writes a 404 invalid_request_error with code resource_missing.
func NotFound(c *gin.Context, message, param string) {
	Write(c, http.StatusNotFound, InvalidRequest(CodeResourceMissing, message, param))
}

// Conflict writes a 409 invalid_request_error.
func Conflict(c *gin.Context, code, message, param string) {
	Write(c, http.StatusConflict, InvalidRequest(code, message, param))
}

// InternalServerError writes a generic 500 api_error. Pass the error that
// caused it: this is where a failed request's error is handled, so it is
// recorded on the request span (see telemetry.RecordError).
func InternalServerError(c *gin.Context, errs ...error) {
	recordErrors(c, errs)
	Write(c, http.StatusInternalServerError, Internal())
}

// AbortInternal aborts with a generic 500 api_error, recording err on the
// request span like InternalServerError.
func AbortInternal(c *gin.Context, err error) {
	recordErrors(c, []error{err})
	Abort(c, http.StatusInternalServerError, Internal())
}

// GatewayTimeout writes a 504 api_error, recording the errors passed like
// InternalServerError.
func GatewayTimeout(c *gin.Context, errs ...error) {
	recordErrors(c, errs)
	Write(c, http.StatusGatewayTimeout, Error{Type: TypeAPI, Code: CodeTimeout, Message: "The request timed out."})
}

// recordErrors records errs on the request span: once per error, and only an
// Error status for errors already recorded where they originated.
func recordErrors(c *gin.Context, errs []error) {
	for _, err := range errs {
		telemetry.RecordError(c.Request.Context(), err)
	}
}
