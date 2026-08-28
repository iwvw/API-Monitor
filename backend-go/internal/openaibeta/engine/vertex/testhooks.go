package vertex

import "github.com/iwvw/api-monitor/backend-go/internal/openaibeta/engine/recaptcha"

// SetBatchGraphqlURL overrides the batchGraphql URL for testing.
// Will be replaced by dependency injection in phase 3/4.
func SetBatchGraphqlURL(url string) {
	batchGraphqlURL = url
}

// SetTokenPool replaces the token pool for testing.
// Will be replaced by dependency injection in phase 3/4.
func (c *VertexAIClient) SetTokenPool(pool *recaptcha.TokenPool) {
	c.pool = pool
}
