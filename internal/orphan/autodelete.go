package orphan

import (
	"os"
	"strconv"
	"strings"
)

// AutoDeleteMode controls the janitor's orphaned-resource auto-remediation.
type AutoDeleteMode string

const (
	// AutoDeleteOff disables auto-remediation entirely (default).
	AutoDeleteOff AutoDeleteMode = "off"
	// AutoDeleteDryRun evaluates the safety gate and logs/audits exactly what
	// WOULD be deleted, without deleting anything.
	AutoDeleteDryRun AutoDeleteMode = "dryrun"
	// AutoDeleteOn deletes every gate-passing orphaned resource.
	AutoDeleteOn AutoDeleteMode = "on"
)

// DefaultAutoDeleteMaxPerCycle bounds how many resources auto-remediation will
// actually delete in a single janitor cycle, limiting blast radius.
const DefaultAutoDeleteMaxPerCycle = 20

// AutoDeleteModeFromEnv parses ORPHAN_AUTO_DELETE. Recognized values:
//
//	off | false | 0 | ""  -> AutoDeleteOff (default)
//	dryrun | dry-run | dry -> AutoDeleteDryRun
//	on | true | 1          -> AutoDeleteOn
//
// Anything unrecognized is treated as off (fail safe).
func AutoDeleteModeFromEnv() AutoDeleteMode {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("ORPHAN_AUTO_DELETE"))) {
	case "on", "true", "1":
		return AutoDeleteOn
	case "dryrun", "dry-run", "dry":
		return AutoDeleteDryRun
	default:
		return AutoDeleteOff
	}
}

// AutoDeleteMaxPerCycleFromEnv reads ORPHAN_AUTO_DELETE_MAX_PER_CYCLE, falling
// back to DefaultAutoDeleteMaxPerCycle when unset or unparseable. A value <= 0
// also falls back to the default.
func AutoDeleteMaxPerCycleFromEnv() int {
	if v := os.Getenv("ORPHAN_AUTO_DELETE_MAX_PER_CYCLE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return DefaultAutoDeleteMaxPerCycle
}
