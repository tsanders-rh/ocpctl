// Package olm holds small, generic OLM (Operator Lifecycle Manager) helpers used
// by the bare-metal operator installs. It is workload-agnostic.
package olm

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const marketplaceNS = "openshift-marketplace"

// Runner is the subset of the host client olm needs (oc on the host).
type Runner interface {
	RunCapture(ctx context.Context, cmd string) (string, error)
}

var stableRE = regexp.MustCompile(`^stable-(\d+)\.(\d+)$`)

// ResolveChannel picks the best available Subscription channel for pkg from its
// PackageManifest, so an operator install is resilient to a catalog that does not
// carry the exact channel that tracks the cluster's OCP minor (as happens on
// pre-release catalogs). Preference order: the desired channel if the catalog
// offers it; else the catalog's own defaultChannel; else the newest stable-X.Y by
// version. ocCmd is the oc invocation prefix (e.g. "<oc> --kubeconfig=<kc>").
func ResolveChannel(ctx context.Context, r Runner, ocCmd, pkg, desired string) (string, error) {
	return resolveChannel(ctx, r, time.Sleep, ocCmd, pkg, desired)
}

func resolveChannel(ctx context.Context, r Runner, sleep func(time.Duration), ocCmd, pkg, desired string) (string, error) {
	// The marketplace catalog may still be syncing on a fresh cluster; poll for
	// the PackageManifest to appear. defaultChannel and the channel names are
	// emitted as "<default>|<name>,<name>,...".
	query := fmt.Sprintf(
		"%s get packagemanifest %s -n %s -o jsonpath='{.status.defaultChannel}|{range .status.channels[*]}{.name},{end}'",
		ocCmd, pkg, marketplaceNS)
	var out string
	for i := 0; i < 30; i++ {
		o, err := r.RunCapture(ctx, query)
		if err == nil && strings.Contains(o, "|") && strings.TrimSpace(strings.TrimSuffix(o, "|")) != "" {
			out = o
			break
		}
		sleep(10 * time.Second)
	}
	if out == "" {
		return "", fmt.Errorf("olm: PackageManifest %s not found in %s (catalog not ready or package absent)", pkg, marketplaceNS)
	}

	def, channels := parsePackageManifest(out)
	if containsStr(channels, desired) {
		return desired, nil
	}
	if def != "" && containsStr(channels, def) {
		return def, nil
	}
	if n := newestStable(channels); n != "" {
		return n, nil
	}
	if def != "" {
		return def, nil
	}
	if len(channels) > 0 {
		return channels[len(channels)-1], nil
	}
	return "", fmt.Errorf("olm: PackageManifest %s lists no channels", pkg)
}

func parsePackageManifest(out string) (defaultChannel string, channels []string) {
	parts := strings.SplitN(strings.TrimSpace(out), "|", 2)
	defaultChannel = strings.TrimSpace(parts[0])
	if len(parts) == 2 {
		for _, c := range strings.Split(parts[1], ",") {
			if c = strings.TrimSpace(c); c != "" {
				channels = append(channels, c)
			}
		}
	}
	return defaultChannel, channels
}

// newestStable returns the highest stable-X.Y channel by (major, minor), or "".
func newestStable(channels []string) string {
	best := ""
	bestMaj, bestMin := -1, -1
	for _, c := range channels {
		m := stableRE.FindStringSubmatch(c)
		if m == nil {
			continue
		}
		maj, merr := strconv.Atoi(m[1])
		min, nerr := strconv.Atoi(m[2])
		if merr != nil || nerr != nil {
			continue
		}
		if maj > bestMaj || (maj == bestMaj && min > bestMin) {
			best, bestMaj, bestMin = c, maj, min
		}
	}
	return best
}

func containsStr(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
