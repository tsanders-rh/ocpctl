package types

import "fmt"

// NotReadyError indicates a job should be deferred because a precondition is not met
type NotReadyError struct {
	Resource string
	Current  string
	Required string
}

func (e *NotReadyError) Error() string {
	return fmt.Sprintf("%s is not ready: current state=%s, required=%s", e.Resource, e.Current, e.Required)
}

// IsNotReadyError checks if an error is a NotReadyError
func IsNotReadyError(err error) bool {
	_, ok := err.(*NotReadyError)
	return ok
}

// TransientError indicates a job failed due to a transient condition and should be retried with backoff
// Examples: AWS rate limits, NAT gateway timing issues, API server temporarily unreachable
type TransientError struct {
	Message     string // Human-readable error message
	Cause       error  // Underlying error that caused the transient failure
	Remediation string // Optional guidance for users
	BackoffMins int    // Suggested backoff in minutes (0 = use default exponential backoff)
}

func (e *TransientError) Error() string {
	if e.Remediation != "" {
		return fmt.Sprintf("%s\n\n%s", e.Message, e.Remediation)
	}
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Cause)
	}
	return e.Message
}

func (e *TransientError) Unwrap() error {
	return e.Cause
}

// IsTransientError checks if an error is a TransientError
func IsTransientError(err error) bool {
	_, ok := err.(*TransientError)
	return ok
}
