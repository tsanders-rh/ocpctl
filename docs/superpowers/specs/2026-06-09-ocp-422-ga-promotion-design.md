# OCP 4.22.0 GA Promotion

**Date:** 2026-06-09
**Status:** Approved
**Approach:** Full GA Promotion (Approach A)

## Goal

Promote OCP 4.22 from prerelease (`4.22.0-ec.5`) to GA (`4.22.0`) across all ocpctl provisioning configuration. Make `4.22.0` the new default version on all applicable profiles.

## Scope

~25 files across 4 groups. No changes to Go runtime logic (`installer.go` already supports 4.22 and its dev-preview detection keys off `-ec.`/`-rc.` suffixes).

## Group 1: GA Profiles — Add `4.22.0` to allowlist, set as default

These profiles currently top out at `4.21.10`. Add `"4.22.0"` to the `openshiftVersions.allowlist` and change `default` to `"4.22.0"`.

| File | Previous default |
|------|-----------------|
| `internal/profile/definitions/aws-standard-ga.yaml` | 4.21.10 |
| `internal/profile/definitions/aws-minimal-ga.yaml` | 4.21.10 |
| `internal/profile/definitions/aws-sno-ga.yaml` | 4.21.10 |
| `internal/profile/definitions/aws-virtualization-ga.yaml` | 4.21.10 |
| `internal/profile/definitions/aws-virt-windows-minimal-ga.yaml` | 4.21.10 |
| `internal/profile/definitions/aws-virt-windows-minimal-ga-s3test.yaml` | 4.21.10 |
| `internal/profile/definitions/aws-rosa-standard.yaml` | 4.21.10 |
| `internal/profile/definitions/aws-rosa-minimal.yaml` | 4.21.10 |
| `internal/profile/definitions/aws-rhwa-lab.yaml` | 4.21.10 |
| `internal/profile/definitions/gcp-standard.yaml` | 4.20.3 |
| `internal/profile/definitions/gcp-sno-ga.yaml` | 4.20.3 |

## Group 2: Prerelease Profiles — Replace `4.22.0-ec.5` with `4.22.0`

These profiles have `4.22.0-ec.5` as their only allowlist entry and default. Replace with `4.22.0` in both fields. (They'll sit idle until a 4.23 EC appears.)

| File |
|------|
| `internal/profile/definitions/aws-standard-prerelease.yaml` |
| `internal/profile/definitions/aws-minimal-prerelease.yaml` |
| `internal/profile/definitions/aws-sno-prerelease.yaml` |
| `internal/profile/definitions/aws-virtualization-prerelease.yaml` |
| `internal/profile/definitions/aws-virt-windows-minimal-prerelease.yaml` |

## Group 3: Combined/Test Profiles — Replace EC with GA, update default

These profiles have a mix of GA and prerelease versions. Replace `4.22.0-ec.5` with `4.22.0` in allowlist. Update default to `4.22.0`.

| File | Previous default |
|------|-----------------|
| `internal/profile/definitions/aws-standard.yaml` (disabled) | 4.20.3 |
| `internal/profile/definitions/aws-minimal-test.yaml` | 4.20.3 |
| `internal/profile/definitions/aws-sno-test.yaml` | 4.20.3 |
| `internal/profile/definitions/aws-sno-422-test.yaml` (disabled) | 4.22.0-ec.5 |
| `internal/profile/definitions/aws-sno-shared-vpc.yaml` | 4.20.3 |
| `internal/profile/definitions/aws-sno-shared-vpc-custom.yaml` | 4.20.3 |
| `internal/profile/definitions/aws-virtualization.yaml` | 4.20.3 |
| `internal/profile/definitions/aws-virt-windows-minimal.yaml` | 4.20.3 |

Also update the `aws-sno-422-test.yaml` description from EC to GA.

## Group 4: Scripts, Jenkins, and Documentation

### scripts/ensure-installers.sh
- Line 17: `DEFAULT_PATCHES["4.22"]="4.22.0-ec.5"` → `DEFAULT_PATCHES["4.22"]="4.22.0"`

### scripts/install-multiversion-binaries.sh
- Line 3 comment: remove "dev-preview" for 4.22
- Line 18 echo: change "Developer Preview - Early Candidate" to GA description
- Lines 43-46: change `MIRROR_PATH` from `"ocp-dev-preview"` to `"ocp"`, `FULL_VERSION` from `"4.22.0-ec.5"` to `"4.22.0"`

### examples/jenkins/Jenkinsfile
- Add `"4.22.0"` at the top of the `OPENSHIFT_VERSION` choices list

### scripts/update-pull-secret.sh
- Line 125: change example version from `4.22.0-ec.5` to `4.22.0`

### docs/setup/MULTIVERSION_SETUP.md
- Update "Supported Dev-Preview Versions" section to note 4.22 is now GA
- Update download examples from `ocp-dev-preview/4.22.0-ec.5` to `ocp/4.22.0`
- Update profile configuration example

## Not Changed

- **`internal/installer/installer.go`** — `supportedVersions` already includes `"4.22"`. Dev-preview detection logic (`-ec.`, `-rc.`, `-0.nightly`) correctly identifies GA versions (no suffix) as non-dev-preview.
- **`internal/installer/installer_test.go`** — Existing EC test cases remain valid for testing prerelease parsing logic.
- **`internal/profile/updater.go`** — Line 347 comment mentions `4.22.0-ec.5` as a format example; still valid documentation.
- **Azure profiles** — Use minor-only versions (e.g., `"4.22"`), already have 4.22 support.
- **IBM Cloud profiles** — Capped at 4.20, not applicable.
- **EKS/IKS/GKE profiles** — Non-OCP, not applicable.
