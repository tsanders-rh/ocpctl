package store

import "testing"

func TestNormalizeIP(t *testing.T) {
	ptr := func(s string) *string { return &s }

	cases := []struct {
		name string
		in   *string
		want *string // nil means SQL NULL
	}{
		{"nil stays nil", nil, nil},
		{"valid ipv4", ptr("104.190.230.9"), ptr("104.190.230.9")},
		{"ipv4 with surrounding space", ptr("  10.0.0.5 "), ptr("10.0.0.5")},
		{"valid ipv6 canonicalized", ptr("2001:DB8::1"), ptr("2001:db8::1")},
		// The bug: a crafted proxy header must not blow up the inet cast.
		{"malformed backslash prefix -> NULL", ptr(`\104.190.230.9`), nil},
		{"empty string -> NULL", ptr(""), nil},
		{"hostname -> NULL", ptr("not-an-ip"), nil},
		{"ip:port -> NULL", ptr("10.0.0.5:5678"), nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeIP(tc.in)
			switch {
			case tc.want == nil && got != nil:
				t.Fatalf("expected nil, got %q", *got)
			case tc.want != nil && got == nil:
				t.Fatalf("expected %q, got nil", *tc.want)
			case tc.want != nil && got != nil && *got != *tc.want:
				t.Fatalf("expected %q, got %q", *tc.want, *got)
			}
		})
	}
}
