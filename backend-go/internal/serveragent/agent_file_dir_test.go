package serveragent

import "testing"

func TestJoinPosixPath(t *testing.T) {
	cases := []struct {
		base, name, want string
	}{
		{"/a/b", "c", "/a/b/c"},
		{"/a/b/", "c", "/a/b/c"},
		{"/a/b", "", "/a/b"},
		{"", "c", "c"},
		{"/a", "d/e", "/a/d/e"},
		{"C:/x", "y", "C:/x/y"},
	}
	for _, c := range cases {
		if got := joinPosixPath(c.base, c.name); got != c.want {
			t.Fatalf("joinPosixPath(%q,%q)=%q want %q", c.base, c.name, got, c.want)
		}
	}
}
