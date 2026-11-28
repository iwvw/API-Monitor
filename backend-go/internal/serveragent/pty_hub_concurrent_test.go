package serveragent

import (
	"sync"
	"testing"
)

func TestPtyDataHubPublishUnsubscribeRace(t *testing.T) {
	const (
		rounds      = 300
		subscribers = 16
		publishes   = 16
	)
	hub := newPtyDataHub()

	for r := 0; r < rounds; r++ {
		cancels := make([]func(), 0, subscribers)
		for i := 0; i < subscribers; i++ {
			_, cancel := hub.Subscribe("pty-race")
			cancels = append(cancels, cancel)
		}

		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			for j := 0; j < publishes; j++ {
				hub.Publish("pty-race", "chunk")
			}
		}()
		go func() {
			defer wg.Done()
			<-start
			for _, cancel := range cancels {
				cancel()
			}
		}()
		close(start)
		wg.Wait()
	}
}

func TestPtyDataHubPublishAfterUnsubscribeIsNoop(t *testing.T) {
	hub := newPtyDataHub()
	_, cancel := hub.Subscribe("pty-gone")
	cancel()
	if hub.Publish("pty-gone", "chunk") {
		t.Fatalf("publish after unsubscribe must report no delivery")
	}
}
