package textclean

import "testing"

func TestStripReferenceMarkers(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty",
			input:    "",
			expected: "",
		},
		{
			name:     "no markers",
			input:    "Hello world, no markers here.",
			expected: "Hello world, no markers here.",
		},
		{
			name:     "citation marker",
			input:    "According to research[citation: 1], the sky is blue.",
			expected: "According to research, the sky is blue.",
		},
		{
			name:     "reference marker",
			input:    "According to research[reference:42], the sky is blue.",
			expected: "According to research, the sky is blue.",
		},
		{
			name:     "case insensitive",
			input:    "Fact[CITATION: 2] and fact[Reference: 3].",
			expected: "Fact and fact.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StripReferenceMarkers(tt.input)
			if got != tt.expected {
				t.Fatalf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}

func TestStripReferenceMarkersEnabled(t *testing.T) {
	if !StripReferenceMarkersEnabled() {
		t.Fatalf("expected StripReferenceMarkersEnabled() to be true")
	}
}
