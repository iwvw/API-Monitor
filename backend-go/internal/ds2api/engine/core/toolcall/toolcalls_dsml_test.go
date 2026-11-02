package toolcall

import (
	"strings"
	"testing"
)

// dsmlPipe is U+FF5C fullwidth vertical line; the web-agent frames double it
// around the literal DSML segment.
const dsmlPipe = "｜"

func dsmlSample() string {
	dd := dsmlPipe + dsmlPipe
	return strings.Join([]string{
		"<" + dd + "DSML" + dd + " calls>",
		"<" + dd + "DSML" + dd + " invoke name=\"Bash\">",
		"<" + dd + "DSML" + dd + " parameter name=\"command\" string=\"true\">cd /repo && git diff</" + dd + "DSML" + dd + " parameter>",
		"<" + dd + "DSML" + dd + " parameter name=\"description\" string=\"true\">Show diff</" + dd + "DSML" + dd + " parameter>",
		"</" + dd + "DSML" + dd + " invoke>",
		"<" + dd + "DSML" + dd + " invoke name=\"Bash\">",
		"<" + dd + "DSML" + dd + " parameter name=\"command\" string=\"true\">git status</" + dd + "DSML" + dd + " parameter>",
		"<" + dd + "DSML" + dd + " parameter name=\"description\" string=\"true\">List changes</" + dd + "DSML" + dd + " parameter>",
		"</" + dd + "DSML" + dd + " invoke>",
		"</" + dd + "DSML" + dd + " calls>",
	}, "\n")
}

func TestRewriteDSMLWrapperFrames(t *testing.T) {
	rewritten := RewriteDSMLWrapperFrames(dsmlSample())
	for _, want := range []string{"<tool_calls>", "</tool_calls>", "<invoke name=\"Bash\">", "</invoke>", "<parameter name=\"command\""} {
		if !strings.Contains(rewritten, want) {
			t.Fatalf("expected %q in rewritten text, got: %s", want, rewritten)
		}
	}
	if strings.Contains(rewritten, "DSML") {
		t.Fatalf("DSML prefix should be stripped, got: %s", rewritten)
	}
}

func TestParseDSMLWrapperFrames(t *testing.T) {
	res := ParseStandaloneToolCallsDetailed(dsmlSample(), []string{"Bash"})
	if len(res.Calls) != 2 {
		t.Fatalf("expected 2 parsed calls, got %d calls=%#v", len(res.Calls), res.Calls)
	}
	if !res.SawToolCallSyntax {
		t.Fatal("expected SawToolCallSyntax to be true so phase-3 repair can fire")
	}
	for i, tc := range res.Calls {
		if tc.Name != "Bash" {
			t.Fatalf("call %d: expected name Bash, got %q", i, tc.Name)
		}
		cmd, _ := tc.Input["command"].(string)
		if strings.TrimSpace(cmd) == "" {
			t.Fatalf("call %d: expected non-empty command input, got %#v", i, tc.Input)
		}
	}
}

func TestRewriteDSMLWrapperFramesLeavesOrdinaryTextAlone(t *testing.T) {
	ordinary := "function calls happen; see <calls> in docs"
	if got := RewriteDSMLWrapperFrames(ordinary); got != ordinary {
		t.Fatalf("ordinary text must be untouched, got %q", got)
	}
}
