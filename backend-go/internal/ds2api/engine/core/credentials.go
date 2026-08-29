package config

import (
	"slices"
	"strings"
)

func (c *Config) ReconcileCredentials(base Config) {
	if c == nil {
		return
	}
	currKeys := normalizeKeys(c.Keys)
	currAPIKeys := normalizeAPIKeys(c.APIKeys)
	baseKeys := normalizeKeys(base.Keys)
	baseAPIKeys := normalizeAPIKeys(base.APIKeys)

	keysChanged := !slices.Equal(currKeys, baseKeys)
	apiKeysChanged := !equalAPIKeys(currAPIKeys, baseAPIKeys)

	if keysChanged && !apiKeysChanged {
		c.APIKeys = apiKeysFromStrings(currKeys, apiKeyMap(baseAPIKeys))
	} else {
		c.APIKeys = currAPIKeys
	}
	c.Keys = apiKeysToStrings(c.APIKeys)
}

func normalizeKeys(keys []string) []string {
	if len(keys) == 0 {
		return nil
	}
	out := make([]string, 0, len(keys))
	seen := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, key)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizeAPIKeys(items []APIKey) []APIKey {
	if len(items) == 0 {
		return nil
	}
	out := make([]APIKey, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		key := strings.TrimSpace(item.Key)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, APIKey{
			Key:    key,
			Name:   strings.TrimSpace(item.Name),
			Remark: strings.TrimSpace(item.Remark),
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func apiKeysFromStrings(keys []string, meta map[string]APIKey) []APIKey {
	if len(keys) == 0 {
		return nil
	}
	out := make([]APIKey, 0, len(keys))
	seen := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		if item, ok := meta[key]; ok {
			out = append(out, APIKey{
				Key:    key,
				Name:   strings.TrimSpace(item.Name),
				Remark: strings.TrimSpace(item.Remark),
			})
			continue
		}
		out = append(out, APIKey{Key: key})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func apiKeysToStrings(items []APIKey) []string {
	if len(items) == 0 {
		return nil
	}
	keys := make([]string, 0, len(items))
	for _, item := range items {
		key := strings.TrimSpace(item.Key)
		if key == "" {
			continue
		}
		keys = append(keys, key)
	}
	if len(keys) == 0 {
		return nil
	}
	return keys
}

func apiKeyMap(items []APIKey) map[string]APIKey {
	if len(items) == 0 {
		return nil
	}
	out := make(map[string]APIKey, len(items))
	for _, item := range items {
		key := strings.TrimSpace(item.Key)
		if key == "" {
			continue
		}
		if _, ok := out[key]; ok {
			continue
		}
		out[key] = APIKey{
			Key:    key,
			Name:   strings.TrimSpace(item.Name),
			Remark: strings.TrimSpace(item.Remark),
		}
	}
	return out
}

func equalAPIKeys(a, b []APIKey) bool {
	if len(a) != len(b) {
		return false
	}
	return slices.EqualFunc(a, b, func(x, y APIKey) bool {
		return strings.TrimSpace(x.Key) == strings.TrimSpace(y.Key) &&
			strings.TrimSpace(x.Name) == strings.TrimSpace(y.Name) &&
			strings.TrimSpace(x.Remark) == strings.TrimSpace(y.Remark)
	})
}
