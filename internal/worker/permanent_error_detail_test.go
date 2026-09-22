package worker

import (
	"errors"
	"strings"
	"testing"
)

// The detail attached to a permanent Azure failure must name the resource the
// installer could not find, because that is the whole diagnostic value of the
// message shown on the Jobs card.
//
// Regression for octl-mman-b0291c2-322 (prod, 2026-09-22): a CI Azure create
// failed and the stored error_message read, in full,
//
//	Permanent failure (no retry): Azure resource group not found (check the
//	profile's base-domain / DNS zone resource group)
//
//	Detail: level=error msg=    "code": "ResourceGroupNotFound",
//
// The classification was correct, but the detail was a bare JSON fragment. The
// resource-group name lives on the *next* line of the block, so line-at-a-time
// matching dropped it and an operator had to go read the installer log to learn
// which group was missing (it was azure-mg-dog8code-com-dns, whose Azure DNS
// zone no longer exists).
//
// The pre-existing tests all used single-line errors where the marker and the
// resource name share a line, which is why this shape was never covered.
func TestPermanentErrorDetailNamesTheMissingResource(t *testing.T) {
	// How openshift-install actually renders an Azure SDK error: pretty-printed
	// JSON, one `level=error msg=` line per JSON line.
	azureBlock := errors.New(
		`time="2026-09-22T05:33:41Z" level=info msg=Credentials loaded from target cluster` + "\n" +
			`time="2026-09-22T05:33:42Z" level=error msg=Error: creating DNS record set: performing Get: unexpected status 404` + "\n" +
			`time="2026-09-22T05:33:42Z" level=error msg={` + "\n" +
			`time="2026-09-22T05:33:42Z" level=error msg=  "error": {` + "\n" +
			`time="2026-09-22T05:33:42Z" level=error msg=    "code": "ResourceGroupNotFound",` + "\n" +
			`time="2026-09-22T05:33:42Z" level=error msg=    "message": "Resource group 'azure-mg-dog8code-com-dns' could not be found."` + "\n" +
			`time="2026-09-22T05:33:42Z" level=error msg=  }` + "\n" +
			`time="2026-09-22T05:33:42Z" level=error msg=}`)

	cause, detail := DetectPermanentError(azureBlock)
	if cause == "" {
		t.Fatal("expected permanent classification, got none")
	}

	if !strings.Contains(detail, "azure-mg-dog8code-com-dns") {
		t.Fatalf("detail must name the missing resource group, got %q", detail)
	}
	// The code is what makes the failure searchable; keep it alongside.
	if !strings.Contains(detail, "ResourceGroupNotFound") {
		t.Fatalf("detail should retain the error code, got %q", detail)
	}
	// Must still fail fast rather than burn retries.
	if te := DetectTransientError(azureBlock); te != nil {
		t.Fatalf("expected non-transient, got %+v", te)
	}
}

func TestAdjacentJSONMessageIsNarrow(t *testing.T) {
	t.Run("single-line error is unchanged", func(t *testing.T) {
		// The marker and the resource name already share a line; nothing to add.
		err := errors.New("ResourceGroupNotFound: Resource group 'os4-common' could not be found")
		_, detail := DetectPermanentError(err)
		if detail != "ResourceGroupNotFound: Resource group 'os4-common' could not be found" {
			t.Fatalf("single-line detail should pass through verbatim, got %q", detail)
		}
	})

	t.Run("does not reach into an unrelated later block", func(t *testing.T) {
		// A "message" far below the matched code line belongs to some other
		// error; the window must not pull it in.
		lines := []string{
			`level=error msg=    "code": "ResourceGroupNotFound",`,
		}
		for i := 0; i < 10; i++ {
			lines = append(lines, `level=info msg=unrelated progress line`)
		}
		lines = append(lines, `level=error msg=    "message": "Something entirely different failed."`)

		_, detail := DetectPermanentError(errors.New(strings.Join(lines, "\n")))
		if strings.Contains(detail, "entirely different") {
			t.Fatalf("detail pulled in an unrelated message: %q", detail)
		}
	})

	t.Run("line already carrying a message is not appended to", func(t *testing.T) {
		line := `level=error msg=    "code": "ResourceGroupNotFound", "message": "Resource group 'rg-a' could not be found."`
		next := `level=error msg=    "message": "a different message"`
		_, detail := DetectPermanentError(errors.New(line + "\n" + next))
		if strings.Contains(detail, "a different message") {
			t.Fatalf("should not append when the matched line already has a message: %q", detail)
		}
		if !strings.Contains(detail, "rg-a") {
			t.Fatalf("expected the in-line message to survive, got %q", detail)
		}
	})

	t.Run("other Azure permanent patterns also gain their message", func(t *testing.T) {
		err := errors.New(
			`level=error msg=    "code": "AuthorizationFailed",` + "\n" +
				`level=error msg=    "message": "The client does not have authorization to perform action 'Microsoft.Network/dnsZones/read'."`)
		cause, detail := DetectPermanentError(err)
		if cause == "" {
			t.Fatal("expected permanent classification, got none")
		}
		if !strings.Contains(detail, "Microsoft.Network/dnsZones/read") {
			t.Fatalf("detail should name the denied action, got %q", detail)
		}
	})
}
