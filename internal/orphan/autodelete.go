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

// ParseAutoDeleteMode parses a mode string (canonical or alias) into an
// AutoDeleteMode. ok is false for empty or unrecognized input, so callers can
// distinguish "explicitly off" from "invalid, fall back to the default".
func ParseAutoDeleteMode(s string) (mode AutoDeleteMode, ok bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "off", "false", "0":
		return AutoDeleteOff, true
	case "dryrun", "dry-run", "dry":
		return AutoDeleteDryRun, true
	case "on", "true", "1":
		return AutoDeleteOn, true
	default:
		return AutoDeleteOff, false
	}
}

// AutoDeleteOnAllowed reports whether real deletion (AutoDeleteOn) may be armed
// in this environment. The "on" mode is gated behind an explicit opt-in
// (ORPHAN_AUTO_DELETE_ALLOW_ON=true|1|yes); anything else -- including unset --
// forbids it.
//
// This is an environment interlock, separate from the per-resource safety and
// ownership gates. It exists because an ocpctl environment that shares a cloud
// account with another deployment can classify the OTHER deployment's live
// infrastructure as "orphaned": e.g. dev shares prod's AWS account but has no
// live clusters of its own, so dev's janitor sees prod's live resources as
// orphans. Leaving this flag unset on such an environment guarantees it can
// never actually delete, no matter what the admin console or the worker's
// ORPHAN_AUTO_DELETE env is set to.
func AutoDeleteOnAllowed() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("ORPHAN_AUTO_DELETE_ALLOW_ON"))) {
	case "true", "1", "yes":
		return true
	default:
		return false
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
