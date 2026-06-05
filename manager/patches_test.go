package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// SEC-C16: the exit-table throw-route insertion error in patch 006 must be
// surfaced, not swallowed. The Phase 9 / Task 10.11 fix adds an explicit
// "return err" inside the UBNT-only branch; this guard locks that line so
// a future patch edit can't quietly drop it again.
//
// SEC-C17: only true catch-all prefixes (cidr.Bits() == 0) may land in the
// gated exit-route table 53. Patch 006 originally used "<= 1" which
// trapped /1 subnets too; the fix tightens the predicate to "== 0" and
// this guard locks the literal.
//
// Both guards are intentionally text-level: the patched code lives in a
// fork of Tailscale and is not reachable from this Go module. Locking the
// patch text is cheaper than rebuilding the entire fork inside a test.
func TestPatch006_ContractIsLocked(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	// manager/ → repo root
	root := filepath.Dir(wd)
	patchPath := filepath.Join(root, "patches", "006-ubnt-exit-route-table.patch")
	b, err := os.ReadFile(patchPath)
	if err != nil {
		t.Fatalf("read patch: %v", err)
	}
	text := string(b)

	// SEC-C16: throw-route insert error must propagate inside UBNT branch.
	// We require the explicit + return err line, which only appears in the
	// added-on-UBNT block when the patch is correct.
	if !strings.Contains(text, "THROW ERROR adding %v to exit table") {
		t.Fatal("patch 006 missing exit-table THROW logf marker — SEC-C16 hardening lost")
	}
	if !patchContainsReturnErrAfter(text, "THROW ERROR adding %v to exit table") {
		t.Fatal("patch 006: exit-table throw-route err must be returned, not swallowed (SEC-C16)")
	}

	// SEC-C17: classification predicate must be Bits() == 0, not <= 1.
	if strings.Contains(text, "cidr.Bits() <= 1") {
		t.Fatal("patch 006: routeTableForPrefix still uses Bits() <= 1; must be == 0 (SEC-C17)")
	}
	if !strings.Contains(text, "cidr.Bits() == 0") {
		t.Fatal("patch 006: routeTableForPrefix must use Bits() == 0 (SEC-C17)")
	}
}

// patchContainsReturnErrAfter checks that within the patch lines that
// follow `marker`, a `+\t\t\treturn err` line appears before the closing
// brace of the UBNT block. Walks at most 10 lines forward — enough to
// span the small added block — to avoid false positives elsewhere.
func patchContainsReturnErrAfter(text, marker string) bool {
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		if !strings.Contains(l, marker) {
			continue
		}
		// Scan the next few lines for a "+...return err" line.
		end := i + 10
		if end > len(lines) {
			end = len(lines)
		}
		for j := i + 1; j < end; j++ {
			trimmed := strings.TrimSpace(lines[j])
			if trimmed == "+return err" || strings.HasPrefix(trimmed, "+return err") {
				return true
			}
			// Accept tab-prefixed variants (gofmt-aligned bodies).
			if strings.HasPrefix(lines[j], "+") &&
				strings.Contains(lines[j], "return err") {
				return true
			}
		}
	}
	return false
}
