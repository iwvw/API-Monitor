package main

import (
	"testing"
)

func TestColumnName(t *testing.T) {
	cases := []struct {
		input string
		want  string
		ok    bool
	}{
		{"id INTEGER PRIMARY KEY", "id", true},
		{"name TEXT NOT NULL", "name", true},
		{"\"created_at\" TIMESTAMP", "created_at", true},
		{"PRIMARY KEY (id)", "", false},
		{"CONSTRAINT fk_user FOREIGN KEY (user_id)", "", false},
		{"CHECK (count >= 0)", "", false},
		{"", "", false},
	}

	for _, c := range cases {
		got, ok := columnName(c.input)
		if ok != c.ok || got != c.want {
			t.Errorf("columnName(%q) = (%q, %v), want (%q, %v)", c.input, got, ok, c.want, c.ok)
		}
	}
}

func TestIsSystemTable(t *testing.T) {
	if !isSystemTable("sqlite_master") {
		t.Fatalf("expected sqlite_master to be system table")
	}
	if !isSystemTable("sqlite_sequence") {
		t.Fatalf("expected sqlite_sequence to be system table")
	}
	if isSystemTable("users") {
		t.Fatalf("expected users not to be system table")
	}
}

func TestSplitTopLevel(t *testing.T) {
	s := "col1 TEXT, col2 INTEGER CHECK (col2 > 0), col3 TEXT"
	parts := splitTopLevel(s, ',')
	if len(parts) != 3 {
		t.Fatalf("expected 3 parts, got %d: %v", len(parts), parts)
	}
}
