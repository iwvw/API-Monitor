package openai

import (
	"strings"
	"testing"
)

func TestSanitizeLeakedDSMLFrame(t *testing.T) {
	dd := "｜" + "｜"
	frame := "before prose " +
		"<" + dd + "DSML" + dd + " calls>" +
		"<" + dd + "DSML" + dd + " invoke name=\"Bash\">" +
		"<" + dd + "DSML" + dd + " parameter name=\"command\" string=\"true\">rm -rf /x</" + dd + "DSML" + dd + " parameter>" +
		"</" + dd + "DSML" + dd + " invoke>" +
		"</" + dd + "DSML" + dd + " calls>" +
		" after prose"
	out := sanitizeLeakedOutput(frame)
	if strings.Contains(out, "DSML") || strings.Contains(out, "rm -rf") {
		t.Fatalf("DSML frame leaked through sanitize: %q", out)
	}
	if !strings.Contains(out, "before prose") || !strings.Contains(out, "after prose") {
		t.Fatalf("prose around frame must survive: %q", out)
	}
}
