package serveragent

import (
	"sync"
	"testing"
)

func TestTaskRegistryBroadcastDeliversToSubscribers(t *testing.T) {
	registry := NewTaskRegistry()
	ch, cancel := registry.SubscribeAll()
	defer cancel()

	event := TaskEvent{Type: "created", TaskID: "task-1", Status: TaskPending}
	registry.broadcast(event)

	select {
	case got := <-ch:
		if got.TaskID != event.TaskID || got.Type != event.Type {
			t.Fatalf("broadcast delivered %#v, want %#v", got, event)
		}
	default:
		t.Fatalf("broadcast did not deliver to subscriber")
	}
}

func TestTaskRegistryBroadcastUnsubscribeRace(t *testing.T) {
	const (
		rounds      = 300
		subscribers = 16
		broadcasts  = 16
	)
	registry := NewTaskRegistry()

	for r := 0; r < rounds; r++ {
		cancels := make([]func(), 0, subscribers)
		for i := 0; i < subscribers; i++ {
			_, cancel := registry.SubscribeAll()
			cancels = append(cancels, cancel)
		}

		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			for j := 0; j < broadcasts; j++ {
				registry.broadcast(TaskEvent{Type: "progress", TaskID: "task-race"})
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
