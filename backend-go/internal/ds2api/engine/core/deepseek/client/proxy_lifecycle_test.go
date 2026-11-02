package client

import (
	"net/http"
	"testing"
)

type lifecycleDoer struct {
	closed int
}

func (d *lifecycleDoer) Do(*http.Request) (*http.Response, error) { return nil, nil }
func (d *lifecycleDoer) CloseIdleConnections()                    { d.closed++ }

func TestCloseRequestClientsClosesEveryBundleMember(t *testing.T) {
	items := []*lifecycleDoer{{}, {}, {}, {}}
	closeRequestClients(requestClients{
		regular:   items[0],
		stream:    items[1],
		fallback:  items[2],
		fallbackS: items[3],
	})
	for i, item := range items {
		if item.closed != 1 {
			t.Fatalf("bundle member %d closed %d times", i, item.closed)
		}
	}
}
