package main

import (
	"testing"
)

func TestVersionDefined(t *testing.T) {
	if version == "" {
		t.Fatalf("expected non-empty version")
	}
}
