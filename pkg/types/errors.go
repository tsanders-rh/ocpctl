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
