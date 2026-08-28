package vertex

import "github.com/iwvw/api-monitor/backend-go/internal/openaibeta/engine/transport"

func (c *VertexAIClient) Net() *transport.NetworkClient { return c.net }
