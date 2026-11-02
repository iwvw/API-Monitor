package shared

import (
	"strings"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/config"
)

func normalizeSettingsConfig(c *config.Config) {
	if c == nil {
		return
	}
	c.Admin.PasswordHash = strings.TrimSpace(c.Admin.PasswordHash)
	c.Embeddings.Provider = strings.TrimSpace(c.Embeddings.Provider)
}

func NormalizeSettingsConfig(c *config.Config) {
	normalizeSettingsConfig(c)
}

func validateSettingsConfig(c config.Config) error {
	return config.ValidateConfig(c)
}

func ValidateSettingsConfig(c config.Config) error {
	return validateSettingsConfig(c)
}

func validateRuntimeSettings(runtime config.RuntimeConfig) error {
	return config.ValidateRuntimeConfig(runtime)
}

func ValidateRuntimeSettings(runtime config.RuntimeConfig) error {
	return validateRuntimeSettings(runtime)
}
