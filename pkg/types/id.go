package types

import (
	"fmt"

	"github.com/segmentio/ksuid"
)

// GenerateClusterID generates a unique cluster ID with prefix
func GenerateClusterID() string {
	return fmt.Sprintf("clu_%s", ksuid.New().String())
}

// GenerateJobID generates a unique job ID with prefix
func GenerateJobID() string {
	return fmt.Sprintf("job_%s", ksuid.New().String())
}

// GenerateID generates a generic unique ID (UUID v4)
func GenerateID() string {
	return ksuid.New().String()
}
