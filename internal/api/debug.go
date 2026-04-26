package api

import (
	"log"
	"os"
	"sync"
)

var (
	debugMode     bool
	debugModeOnce sync.Once
)

// isDebugMode returns true if debug mode is enabled via DEBUG environment variable
func isDebugMode() bool {
	debugModeOnce.Do(func() {
		debugMode = os.Getenv("DEBUG") == "true" || os.Getenv("ENVIRONMENT") == "development"
	})
	return debugMode
}

// debugLog logs a message only if debug mode is enabled
func debugLog(format string, v ...interface{}) {
	if isDebugMode() {
		log.Printf("[DEBUG] "+format, v...)
	}
}
