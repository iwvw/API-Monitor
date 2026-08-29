package toolcall

import "testing"

// TestDetectToolCallIntentMatrix mirrors the coverage matrix in
// plan/tool-call-fallback-design-phase1.md §7. Each case feeds a raw text
// fragment directly to DetectToolCallIntent (the residual-probe primitive) and
// asserts whether a paired EPSE open/close shell is detected outside
// CDATA/comments/fences/inline-code.
func TestDetectToolCallIntentMatrix(t *testing.T) {
	cases := []struct {
		name string
		text string
		want bool
	}{
		// §7 row 2: wrapper present but wrong local name, paired EPSE shell.
		{"bad-local-name paired", `<|EPSE|call name="bash">x</|EPSE|call>`, true},
		// §7 row 3: missing wrapper, bare invoke, paired EPSE shell.
		{"missing-wrapper bare invoke paired", `<|EPSE|invoke name="bash">x</|EPSE|invoke>`, true},
		// §7 row 4: closing tag missing local name, still an EPSE close.
		{"closing missing name", `<|EPSE|tool_calls>body</|EPSE>`, true},
		// §7 row 5: open only, no closing -> not paired.
		{"open only no close", `<|EPSE|tool_calls>body`, false},
		// Mirror of row 5: close only, no preceding open -> not paired.
		{"close only no open", `</|EPSE|tool_calls>`, false},
		// Close appears before open -> not paired (open must precede close).
		{"close before open", `</|EPSE|a>text<|EPSE|b>`, false},
		// §7 row 6: canonical, no EPSE prefix -> not intent.
		{"canonical no epse prefix", `<tool_calls>x</tool_calls>`, false},
		// §7 row 7: fenced EPSE literal -> excluded.
		{"markdown fenced", "```\n<|EPSE|tool_calls>x</|EPSE|tool_calls>\n```", false},
		// Inline code span EPSE literal -> excluded.
		{"inline code span", "`<|EPSE|tool_calls>x</|EPSE|tool_calls>`", false},
		// §7 row 8: CDATA-wrapped literal -> excluded (highest priority §3.2).
		{"cdata literal", `<![CDATA[<|EPSE|tool_calls>x</|EPSE|tool_calls>]]>`, false},
		// XML comment wrapped EPSE literal -> excluded.
		{"comment wrapped", `<!-- <|EPSE|a>x</|EPSE|a> -->`, false},
		// §7 row 9: plain prose mentioning tool words -> not intent.
		{"prose mentions tool words", `please call the tool_calls invoke parameter`, false},
		// §7 row 10: JSON form, no EPSE prefix -> not intent.
		{"json form", `{"name":"bash","arguments":{}}`, false},
		// Isolated angle brackets -> not a tag.
		{"isolated angle brackets", `1 < 2 and 3 > 2`, false},
		// Empty input -> false.
		{"empty", ``, false},
		// epseLike trap (§4.1): pipe-prefixed non-EPSE name must NOT trigger.
		{"pipe prefix without epse literal", `<|foo|tool_calls>x</|foo|tool_calls>`, false},
		// "epse" as a substring of a longer word must not match.
		{"epse substring word", `<|epsentence>x</|epsentence>`, false},
		// Fullwidth/angle EPSE variant with empty local name still pairs.
		{"bare EPSE angle variant", `<EPSE>x</EPSE>`, true},
		// Empty-name closing shorthand </|EPSE> after an EPSE open is a valid close.
		{"empty-name closing shorthand", `<|EPSE|invoke name="a">x</|EPSE>`, true},
		// Different local names on open vs close still pair (existence test only).
		{"mismatched open/close names", `<|EPSE|foo>x</|EPSE|bar>`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DetectToolCallIntent(tc.text); got != tc.want {
				t.Fatalf("DetectToolCallIntent(%q) = %v, want %v", tc.text, got, tc.want)
			}
		})
	}
}

// TestSawToolCallIntentMatrix drives the full parseToolCallsDetailedXMLOnly
// pipeline and asserts the residual-level SawToolCallIntent signal, mirroring
// the §7 "new intent signal" column: parses that succeed are deducted and yield
// false, while unparseable EPSE shells surface as true.
func TestSawToolCallIntentMatrix(t *testing.T) {
	cases := []struct {
		name       string
		text       string
		wantIntent bool
		wantCalls  int
	}{
		// §7 row 1: canonical EPSE, parses -> deducted, no residual intent.
		{
			name:       "well-formed EPSE parses",
			text:       `<|EPSE|tool_calls><|EPSE|invoke name="Bash"><|EPSE|parameter name="command"><![CDATA[pwd]]></|EPSE|parameter></|EPSE|invoke></|EPSE|tool_calls>`,
			wantIntent: false,
			wantCalls:  1,
		},
		// §7 row 2: bad local name -> unparseable, residual intent true.
		{
			name:       "bad local name unparseable",
			text:       `<|EPSE|call name="bash">x</|EPSE|call>`,
			wantIntent: true,
			wantCalls:  0,
		},
		// §7 row 3: missing wrapper but bare invoke parses -> deducted, no residual.
		{
			name:       "bare invoke parses (repaired wrapper)",
			text:       `<|EPSE|invoke name="bash"><|EPSE|parameter name="command"><![CDATA[pwd]]></|EPSE|parameter></|EPSE|invoke></|EPSE|tool_calls>`,
			wantIntent: false,
			wantCalls:  1,
		},
		// §7 row 4: closing missing name -> parse fails, residual intent true.
		{
			name:       "closing missing name unparseable",
			text:       `<|EPSE|tool_calls>body</|EPSE>`,
			wantIntent: true,
			wantCalls:  0,
		},
		// §7 row 5: open only, no closing -> not paired, no intent.
		{
			name:       "open only no closing",
			text:       `<|EPSE|tool_calls>body`,
			wantIntent: false,
			wantCalls:  0,
		},
		// §7 row 6: canonical no EPSE -> parses, no residual intent.
		{
			name:       "canonical no epse parses",
			text:       `<tool_calls><invoke name="bash"></invoke></tool_calls>`,
			wantIntent: false,
			wantCalls:  1,
		},
		// §7 row 7: fenced literal -> stripped, no intent, no calls.
		{
			name:       "markdown fenced literal",
			text:       "```\n<|EPSE|tool_calls>x</|EPSE|tool_calls>\n```",
			wantIntent: false,
			wantCalls:  0,
		},
		// §7 row 8: CDATA literal in plain text -> excluded, no intent.
		{
			name:       "cdata literal excluded",
			text:       `<![CDATA[<|EPSE|tool_calls>x</|EPSE|tool_calls>]]>`,
			wantIntent: false,
			wantCalls:  0,
		},
		// §7 row 9: prose -> no intent.
		{
			name:       "prose mentions tool words",
			text:       `talk about tool_calls and invoke and parameter`,
			wantIntent: false,
			wantCalls:  0,
		},
		// §7 row 10: JSON form -> no intent.
		{
			name:       "json form no intent",
			text:       `{"name":"bash","arguments":{}}`,
			wantIntent: false,
			wantCalls:  0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := ParseStandaloneToolCallsDetailed(tc.text, nil)
			if r.SawToolCallIntent != tc.wantIntent {
				t.Fatalf("SawToolCallIntent = %v, want %v (text %q)", r.SawToolCallIntent, tc.wantIntent, tc.text)
			}
			if len(r.Calls) != tc.wantCalls {
				t.Fatalf("len(Calls) = %d, want %d (calls %#v)", len(r.Calls), tc.wantCalls, r.Calls)
			}
		})
	}
}

// TestSawToolCallIntentDeductionScenarios exercises the §4.2 all-failed /
// all-success / partial-failure deduction scenarios, including the wrapper- and
// invoke-granularity boundaries that the residual probe depends on.
func TestSawToolCallIntentDeductionScenarios(t *testing.T) {
	t.Run("all failed keeps intent (0 calls not early-returned)", func(t *testing.T) {
		// Non-empty text, parse yields zero calls: this is the len(parsed)==0
		// path that must NOT skip residual probing.
		r := ParseStandaloneToolCallsDetailed(`<|EPSE|call name="x">y</|EPSE|call>`, nil)
		if len(r.Calls) != 0 {
			t.Fatalf("expected 0 calls, got %d", len(r.Calls))
		}
		if !r.SawToolCallIntent {
			t.Fatalf("expected SawToolCallIntent=true for all-failed EPSE shell")
		}
	})

	t.Run("all success single wrapper deducted shell-and-all", func(t *testing.T) {
		r := ParseStandaloneToolCallsDetailed(`<|EPSE|tool_calls><|EPSE|invoke name="a"><|EPSE|parameter name="p"><![CDATA[1]]></|EPSE|parameter></|EPSE|invoke></|EPSE|tool_calls>`, nil)
		if len(r.Calls) != 1 {
			t.Fatalf("expected 1 call, got %d", len(r.Calls))
		}
		if r.SawToolCallIntent {
			t.Fatalf("expected SawToolCallIntent=false after deducting fully-successful wrapper")
		}
	})

	t.Run("all success two wrappers deducted", func(t *testing.T) {
		r := ParseStandaloneToolCallsDetailed(`<|EPSE|tool_calls><|EPSE|invoke name="a"><|EPSE|parameter name="p"><![CDATA[1]]></|EPSE|parameter></|EPSE|invoke></|EPSE|tool_calls><|EPSE|tool_calls><|EPSE|invoke name="b"><|EPSE|parameter name="q"><![CDATA[2]]></|EPSE|parameter></|EPSE|invoke></|EPSE|tool_calls>`, nil)
		if len(r.Calls) != 2 {
			t.Fatalf("expected 2 calls, got %d", len(r.Calls))
		}
		if r.SawToolCallIntent {
			t.Fatalf("expected SawToolCallIntent=false after deducting both successful wrappers")
		}
	})

	t.Run("partial failure: success wrapper + trailing bad shell keeps intent", func(t *testing.T) {
		// One fully-successful wrapper is deducted shell-and-all; a trailing
		// bad EPSE shell survives as residual intent.
		r := ParseStandaloneToolCallsDetailed(`<|EPSE|tool_calls><|EPSE|invoke name="a"><|EPSE|parameter name="p"><![CDATA[1]]></|EPSE|parameter></|EPSE|invoke></|EPSE|tool_calls><|EPSE|call name="x">z</|EPSE|call>`, nil)
		if len(r.Calls) != 1 {
			t.Fatalf("expected 1 call, got %d", len(r.Calls))
		}
		if !r.SawToolCallIntent {
			t.Fatalf("expected SawToolCallIntent=true for residual bad shell after successful wrapper")
		}
	})

	t.Run("partial failure inside wrapper: bad invoke keeps intent", func(t *testing.T) {
		// A wrapper with one good invoke and one empty-name (failed) invoke is
		// NOT deducted shell-and-all, so its paired EPSE shell remains.
		r := ParseStandaloneToolCallsDetailed(`<|EPSE|tool_calls><|EPSE|invoke name="a"><|EPSE|parameter name="p"><![CDATA[1]]></|EPSE|parameter></|EPSE|invoke><|EPSE|invoke><|EPSE|parameter name="q"><![CDATA[2]]></|EPSE|parameter></|EPSE|invoke></|EPSE|tool_calls>`, nil)
		if len(r.Calls) != 1 {
			t.Fatalf("expected 1 call, got %d", len(r.Calls))
		}
		if !r.SawToolCallIntent {
			t.Fatalf("expected SawToolCallIntent=true when a wrapper has a failed invoke")
		}
	})

	t.Run("bare invoke granularity: 2 invokes, 2nd bad keeps intent", func(t *testing.T) {
		// No explicit tool_calls opening tag -> invoke-granularity deduction.
		r := ParseStandaloneToolCallsDetailed(`<|EPSE|invoke name="a"><|EPSE|parameter name="p"><![CDATA[1]]></|EPSE|parameter></|EPSE|invoke><|EPSE|invoke></|EPSE|invoke></|EPSE|tool_calls>`, nil)
		if len(r.Calls) != 1 {
			t.Fatalf("expected 1 call, got %d", len(r.Calls))
		}
		if !r.SawToolCallIntent {
			t.Fatalf("expected SawToolCallIntent=true for bare-invoke partial failure")
		}
	})

	t.Run("empty text yields no intent", func(t *testing.T) {
		r := ParseStandaloneToolCallsDetailed("   ", nil)
		if r.SawToolCallIntent || len(r.Calls) != 0 {
			t.Fatalf("expected no intent/calls for whitespace-only text, got intent=%v calls=%d", r.SawToolCallIntent, len(r.Calls))
		}
	})
}
