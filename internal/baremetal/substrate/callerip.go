package substrate

import (
	"io"
	"net/http"
	"strings"
	"time"
)

type httpGetter func(url string) (*http.Response, error)

const checkIPURL = "https://checkip.amazonaws.com"

// detectCallerCIDRs returns the caller's public IP as a /32 CIDR. On any failure
// it returns 0.0.0.0/32, which fails safe (opens nothing) rather than opening the
// security group to the world.
func detectCallerCIDRs(get httpGetter) []string {
	resp, err := get(checkIPURL)
	if err != nil {
		return []string{"0.0.0.0/32"}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64))
	if cerr := resp.Body.Close(); cerr != nil && err == nil {
		err = cerr
	}
	if err != nil || resp.StatusCode != http.StatusOK {
		return []string{"0.0.0.0/32"}
	}
	ip := strings.TrimSpace(string(body))
	if ip == "" {
		return []string{"0.0.0.0/32"}
	}
	return []string{ip + "/32"}
}

// defaultHTTPGet is the production httpGetter (short timeout).
func defaultHTTPGet(url string) (*http.Response, error) {
	c := &http.Client{Timeout: 5 * time.Second}
	return c.Get(url)
}
