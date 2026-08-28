package manifest
import "testing"
func TestOpenAIBetaMatch(t *testing.T) {
	cases := []struct{ path, want string }{
		{"/api/openaibeta/settings", "openaibeta"},
		{"/api/openaibeta/v1/chat/completions", "openaibeta-compatible"},
		{"/api/openaibeta/v1/models", "openaibeta-compatible"},
		{"/api/openaibeta/test", "openaibeta"},
	}
	for _, c := range cases {
		r, ok := Match(c.path)
		if !ok { t.Fatalf("no match for %s", c.path); continue }
		if r.Module != c.want { t.Errorf("%s => module %s, want %s", c.path, r.Module, c.want) }
	}
}
