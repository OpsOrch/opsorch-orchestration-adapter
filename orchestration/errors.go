package orchestration

import "fmt"

// ErrorCode represents different types of errors
type ErrorCode string

const (
	ErrorCodeNotFound        ErrorCode = "NOT_FOUND"
	ErrorCodeInvalidRequest  ErrorCode = "INVALID_REQUEST"
	ErrorCodeValidationError ErrorCode = "VALIDATION_ERROR"
	ErrorCodeDependencyError ErrorCode = "DEPENDENCY_ERROR"
	ErrorCodeStorageError    ErrorCode = "STORAGE_ERROR"
	ErrorCodeInternalError   ErrorCode = "INTERNAL_ERROR"
)

// StructuredError provides detailed error information
type StructuredError struct {
	Code    ErrorCode              `json:"code"`
	Message string                 `json:"message"`
	Details map[string]interface{} `json:"details,omitempty"`
}

func (e *StructuredError) Error() string {
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

// NewNotFoundError creates a not found error
func NewNotFoundError(resource, id string) *StructuredError {
	return &StructuredError{
		Code:    ErrorCodeNotFound,
		Message: fmt.Sprintf("%s '%s' not found", resource, id),
		Details: map[string]interface{}{
			"resource": resource,
			"id":       id,
		},
	}
}

// NewValidationError creates a validation error
func NewValidationError(message string, details map[string]interface{}) *StructuredError {
	return &StructuredError{
		Code:    ErrorCodeValidationError,
		Message: message,
		Details: details,
	}
}

// NewDependencyError creates a dependency error
func NewDependencyError(stepID string, unsatisfiedDeps []string) *StructuredError {
	return &StructuredError{
		Code:    ErrorCodeDependencyError,
		Message: fmt.Sprintf("step '%s' has unsatisfied dependencies", stepID),
		Details: map[string]interface{}{
			"stepId":          stepID,
			"unsatisfiedDeps": unsatisfiedDeps,
		},
	}
}

// NewInvalidRequestError creates an invalid request error
func NewInvalidRequestError(message string) *StructuredError {
	return &StructuredError{
		Code:    ErrorCodeInvalidRequest,
		Message: message,
	}
}

// NewStorageError creates a storage error
func NewStorageError(operation string, err error) *StructuredError {
	return &StructuredError{
		Code:    ErrorCodeStorageError,
		Message: fmt.Sprintf("storage operation '%s' failed: %v", operation, err),
		Details: map[string]interface{}{
			"operation": operation,
			"cause":     err.Error(),
		},
	}
}

// NewInternalError creates an internal error
func NewInternalError(message string, err error) *StructuredError {
	details := make(map[string]interface{})
	if err != nil {
		details["cause"] = err.Error()
	}

	return &StructuredError{
		Code:    ErrorCodeInternalError,
		Message: message,
		Details: details,
	}
}
