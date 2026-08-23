package models

import "fmt"

const (
	ErrCodeTimeout           = "SEARCH_TIMEOUT"
	ErrCodeInvalidInput      = "INVALID_INPUT"
	ErrCodeRateLimited       = "RATE_LIMITED"
	ErrCodeUnauthorized      = "UNAUTHORIZED"
	ErrCodeInternal          = "INTERNAL_ERROR"
	ErrCodeContentProcessing = "CONTENT_PROCESSING_FAILED"

	// Invitation-only Labs codes. They are not part of paid Search.
	ErrCodeInvalidReceipt       = "INVALID_RECEIPT"
	ErrCodeEvidenceUnavailable  = "EVIDENCE_UNAVAILABLE"
	ErrCodeExtractorUnavailable = "EXTRACTOR_UNAVAILABLE"
	ErrCodeLLMFailure           = "LLM_FAILURE"
	ErrCodeLLMAuthFailure       = "LLM_AUTH_FAILURE"
	ErrCodeLLMRateLimited       = "LLM_RATE_LIMITED"
)

type ErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ServiceError carries a stable code while retaining a private wrapped cause.
type ServiceError struct {
	Code    string
	Message string
	Err     error
}

func (err *ServiceError) Error() string {
	if err.Err != nil {
		return fmt.Sprintf("%s: %s: %v", err.Code, err.Message, err.Err)
	}
	return fmt.Sprintf("%s: %s", err.Code, err.Message)
}

func (err *ServiceError) Unwrap() error { return err.Err }

func NewServiceError(code, message string, cause error) *ServiceError {
	return &ServiceError{Code: code, Message: message, Err: cause}
}

func (err *ServiceError) ToDetail() *ErrorDetail {
	return &ErrorDetail{Code: err.Code, Message: err.Message}
}
