package store

import "errors"

var (
	// ErrNotFound is returned when a requested record is not found
	ErrNotFound = errors.New("not found")

	// ErrConflict is returned when an operation conflicts with existing data
	ErrConflict = errors.New("conflict")

	// ErrLockHeld is returned when a lock is already held
	ErrLockHeld = errors.New("lock already held")
)
