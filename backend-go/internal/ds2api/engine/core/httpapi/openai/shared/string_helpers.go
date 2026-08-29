package shared

func AsString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
