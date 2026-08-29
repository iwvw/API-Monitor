package toolcall

import (
	"strings"
	"testing"
)

// TestSlotCDATAContentRoundTrip mirrors plan/tool-call-fallback-cdata-slots.md
// §6 coverage matrix: content is slotted with DS_SLOT_{idx} placeholders, the
// repair layer sees only the skeleton, and restoreCDATASlots reproduces the
// input byte-for-byte.
func TestSlotCDATAContentRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		text string
	}{
		{
			name: "long content closed cdata",
			text: `<|EPSE|call name="Bash"><|EPSE|parameter name="command"><![CDATA[` +
				strings.Repeat("A", 40000) + `]]></|EPSE|parameter></|EPSE|call>`,
		},
		{
			name: "short parameter still slotted",
			text: `<|EPSE|parameter name="command"><![CDATA[pwd]]></|EPSE|parameter>`,
		},
		{
			name: "multiple params multiple invokes",
			text: `<|EPSE|invoke name="a"><|EPSE|parameter name="p"><![CDATA[one]]></|EPSE|parameter></|EPSE|invoke>` +
				`<|EPSE|invoke name="b"><|EPSE|parameter name="q"><![CDATA[two]]></|EPSE|parameter></|EPSE|invoke>`,
		},
		{
			name: "content with ]] not closing",
			text: `<![CDATA[array[i]] = value]]>`,
		},
		{
			name: "empty cdata",
			text: `<![CDATA[]]>`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := slotCDATAContent(tc.text)
			if !res.slotted {
				t.Fatalf("expected slotted=true for %q", tc.name)
			}
			// Skeleton must not contain the raw long content.
			for _, slot := range res.slots {
				if len(slot) > 32 && strings.Contains(res.skeleton, slot) {
					t.Fatalf("skeleton still contains raw slot content")
				}
			}
			// Placeholders must use the DS_SLOT_{idx} literal.
			for i := range res.slots {
				if !strings.Contains(res.skeleton, res.token.format(i)) {
					t.Fatalf("skeleton missing placeholder %s", res.token.format(i))
				}
			}
			restored, err := restoreCDATASlots(res.skeleton, res.slots, res.token)
			if err != nil {
				t.Fatalf("restore error: %v", err)
			}
			if restored != tc.text {
				t.Fatalf("round trip mismatch:\n got: %q\nwant: %q", restored, tc.text)
			}
		})
	}
}

// TestSlotCDATAPlaceholderFormat asserts the placeholder literal is exactly
// DS_SLOT_{idx} for the default (non-colliding) case (plan §2.2 / core req 2).
func TestSlotCDATAPlaceholderFormat(t *testing.T) {
	text := `<![CDATA[hello]]><![CDATA[world]]>`
	res := slotCDATAContent(text)
	if !res.slotted {
		t.Fatal("expected slotted=true")
	}
	if res.token.format(0) != "DS_SLOT_0" || res.token.format(1) != "DS_SLOT_1" {
		t.Fatalf("unexpected placeholder tokens: %q %q", res.token.format(0), res.token.format(1))
	}
	if !strings.Contains(res.skeleton, "<![CDATA[DS_SLOT_0]]>") ||
		!strings.Contains(res.skeleton, "<![CDATA[DS_SLOT_1]]>") {
		t.Fatalf("skeleton did not use DS_SLOT_{idx} placeholders: %q", res.skeleton)
	}
}

// TestSlotCDATANoClosedCDATA covers the plan §3 "unclosed / no CDATA" rows:
// nothing to slot means slotted=false and the skeleton equals the input.
func TestSlotCDATANoClosedCDATA(t *testing.T) {
	cases := []string{
		``,
		`plain text with no cdata`,
		`<|EPSE|parameter name="x">bare</|EPSE|parameter>`,
		`<![CDATA[unclosed content with no terminator`,
	}
	for _, text := range cases {
		res := slotCDATAContent(text)
		if res.slotted {
			t.Fatalf("expected slotted=false for %q", text)
		}
		if res.skeleton != text {
			t.Fatalf("skeleton changed for non-slotted input: %q", text)
		}
	}
}

// TestSlotCDATASkipsUnclosedContinuesScan verifies the §2.5 divergence from
// skipXMLIgnoredSection: a well-closed CDATA is slotted, and a trailing unclosed
// opener is skipped (its opener stepped over) without blocking the scan or
// corrupting the already-slotted region.
func TestSlotCDATASkipsUnclosedContinuesScan(t *testing.T) {
	// First CDATA is well-closed; a trailing unclosed opener must be skipped.
	text := `<![CDATA[good]]> then junk <![CDATA[bad-unclosed`
	res := slotCDATAContent(text)
	if !res.slotted {
		t.Fatal("expected the leading closed CDATA to be slotted")
	}
	if len(res.slots) != 1 || res.slots[0] != "good" {
		t.Fatalf("unexpected slots: %#v", res.slots)
	}
	// The trailing unclosed opener must survive verbatim in the skeleton.
	if !strings.Contains(res.skeleton, "<![CDATA[bad-unclosed") {
		t.Fatalf("trailing unclosed opener was not preserved: %q", res.skeleton)
	}
	restored, err := restoreCDATASlots(res.skeleton, res.slots, res.token)
	if err != nil {
		t.Fatalf("restore error: %v", err)
	}
	if restored != text {
		t.Fatalf("round trip mismatch: got %q want %q", restored, text)
	}
}

// TestSlotCDATAAntiCollision covers plan §2.2 / §6 row "content contains
// DS_SLOT_ literal": the guard switches to the control-char delimiter so the
// placeholder cannot be confused with real content, and round-trips cleanly.
func TestSlotCDATAAntiCollision(t *testing.T) {
	text := `<![CDATA[here is DS_SLOT_0 in the content]]>`
	res := slotCDATAContent(text)
	if !res.slotted {
		t.Fatal("expected slotted=true with delimiter switch")
	}
	if res.token.prefix == "" {
		t.Fatalf("expected anti-collision guard to switch delimiter, got plain token")
	}
	if strings.Contains(res.token.format(0), "\x1e") == false {
		t.Fatalf("expected control-char delimiter, got %q", res.token.format(0))
	}
	restored, err := restoreCDATASlots(res.skeleton, res.slots, res.token)
	if err != nil {
		t.Fatalf("restore error: %v", err)
	}
	if restored != text {
		t.Fatalf("round trip mismatch: got %q want %q", restored, text)
	}
}

// TestRestoreCDATASlotsLeakedTokenDegrades covers plan §4 risk 2: a placeholder
// that the repair layer moved outside a CDATA region must trigger an error so
// the caller degrades to the no-slot path (never leaks DS_SLOT_0 downstream).
func TestRestoreCDATASlotsLeakedTokenDegrades(t *testing.T) {
	res := slotCDATAContent(`<![CDATA[secret]]>`)
	if !res.slotted {
		t.Fatal("expected slotted=true")
	}
	// Simulate a repair layer that moved the placeholder outside the CDATA.
	broken := "DS_SLOT_0 <![CDATA[unrelated]]>"
	if _, err := restoreCDATASlots(broken, res.slots, res.token); err == nil {
		t.Fatal("expected error when placeholder leaks outside CDATA region")
	}
}

// TestRestoreCDATASlotsMissingPlaceholderDegrades covers plan §4: a slot whose
// placeholder was dropped by the repair layer must error out.
func TestRestoreCDATASlotsMissingPlaceholderDegrades(t *testing.T) {
	res := slotCDATAContent(`<![CDATA[a]]><![CDATA[b]]>`)
	if !res.slotted || len(res.slots) != 2 {
		t.Fatalf("expected two slots, got %#v", res.slots)
	}
	// Repaired output keeps only the first placeholder.
	broken := "<![CDATA[" + res.token.format(0) + "]]>"
	if _, err := restoreCDATASlots(broken, res.slots, res.token); err == nil {
		t.Fatal("expected error when a placeholder is missing after repair")
	}
}

// TestRestoreCDATASlotsReorderedInvokes covers plan §6 row "repair layer
// reorders invokes": unique-token matching is order-independent.
func TestRestoreCDATASlotsReorderedInvokes(t *testing.T) {
	text := `<![CDATA[first]]>SEP<![CDATA[second]]>`
	res := slotCDATAContent(text)
	if !res.slotted || len(res.slots) != 2 {
		t.Fatalf("expected two slots, got %#v", res.slots)
	}
	// Repair layer swaps the two CDATA regions.
	reordered := "<![CDATA[" + res.token.format(1) + "]]>SEP<![CDATA[" + res.token.format(0) + "]]>"
	restored, err := restoreCDATASlots(reordered, res.slots, res.token)
	if err != nil {
		t.Fatalf("restore error: %v", err)
	}
	want := `<![CDATA[second]]>SEP<![CDATA[first]]>`
	if restored != want {
		t.Fatalf("reordered restore mismatch: got %q want %q", restored, want)
	}
}

// TestRestoreCDATASlotsPassthroughGenuineContent covers plan §4: a CDATA region
// whose content is genuine (not a placeholder) is passed through untouched,
// while the slotted region restores to its original bytes.
func TestRestoreCDATASlotsPassthroughGenuineContent(t *testing.T) {
	res := slotCDATAContent(`<![CDATA[real]]>`)
	// Repaired output adds a brand-new CDATA with genuine (non-placeholder)
	// content; it must pass through untouched.
	repaired := "<![CDATA[" + res.token.format(0) + "]]><![CDATA[genuine new content]]>"
	restored, err := restoreCDATASlots(repaired, res.slots, res.token)
	if err != nil {
		t.Fatalf("restore error: %v", err)
	}
	want := `<![CDATA[real]]><![CDATA[genuine new content]]>`
	if restored != want {
		t.Fatalf("passthrough mismatch: got %q want %q", restored, want)
	}
}
