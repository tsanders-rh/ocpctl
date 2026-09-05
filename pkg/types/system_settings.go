package types

import "time"

// System settings keys stored in the system_settings table. Keep these in one
// place so the API (writer) and the janitor (reader/telemetry-writer) agree.
const (
	// SettingOrphanAutoRemediation holds the operator-controlled auto-remediation
	// config (OrphanAutoRemediationSettings). When present it OVERRIDES the
	// janitor's ORPHAN_AUTO_DELETE* env bootstrap defaults.
	SettingOrphanAutoRemediation = "orphan_auto_remediation"

	// SettingOrphanAutoRemediationStatus holds per-cycle telemetry the janitor
	// writes at the end of every auto-remediation pass (OrphanAutoRemediationStatus).
	SettingOrphanAutoRemediationStatus = "orphan_auto_remediation_status"
)

// OrphanAutoRemediationSettings is the console-editable configuration for the
// janitor's orphaned-resource auto-remediation. Mode is one of off|dryrun|on.
type OrphanAutoRemediationSettings struct {
	Mode        string `json:"mode"`
	MaxPerCycle int    `json:"maxPerCycle"`
}

// OrphanAutoRemediationStatus is the per-cycle telemetry the janitor writes so
// the admin console can show what the last auto-remediation pass actually did --
// including which effective mode was in force, so the console reflects reality
// even when config still comes from the worker's env bootstrap default.
type OrphanAutoRemediationStatus struct {
	LastRunAt      time.Time `json:"lastRunAt"`
	Mode           string    `json:"mode"`
	Evaluated      int       `json:"evaluated"`
	WouldDelete    int       `json:"wouldDelete"`
	Deleted        int       `json:"deleted"`
	Failed         int       `json:"failed"`
	SkippedUnsafe  int       `json:"skippedUnsafe"`
	SkippedUnowned int       `json:"skippedUnowned"`
	Capped         bool      `json:"capped"`
}
