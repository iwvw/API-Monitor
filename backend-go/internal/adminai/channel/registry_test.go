package channel

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

type fakeChannel struct {
	id string
}

func (f fakeChannel) ID() string            { return f.id }
func (f fakeChannel) Start(context.Context) error {
	return nil
}
func (f fakeChannel) Stop(context.Context) error { return nil }
func (f fakeChannel) Send(context.Context, string, OutboundMessage) (string, error) {
	return "", nil
}
func (f fakeChannel) Edit(context.Context, string, string, OutboundMessage) error { return nil }
func (f fakeChannel) Delete(context.Context, string, string) error                { return nil }
func (f fakeChannel) Status() ChannelStatus                                       { return ChannelStatus{} }

func TestRegistryRegisterGetUnregister(t *testing.T) {
	registry := NewRegistry()
	a := fakeChannel{id: "a"}
	b := fakeChannel{id: "b"}

	registry.Register(a)
	registry.Register(b)

	got, ok := registry.Get("a")
	if !ok || got.ID() != "a" {
		t.Fatalf("get a = %v/%v", got, ok)
	}
	if len(registry.All()) != 2 {
		t.Fatalf("all length = %d, want 2", len(registry.All()))
	}
	if _, ok := registry.Get("missing"); ok {
		t.Fatal("missing channel should not be found")
	}

	registry.Unregister("a")
	if _, ok := registry.Get("a"); ok {
		t.Fatal("channel still registered after unregister")
	}
	if len(registry.All()) != 1 {
		t.Fatalf("all length after unregister = %d, want 1", len(registry.All()))
	}

	registry.Unregister("never-registered")
}

func TestRegistryOverrideByID(t *testing.T) {
	registry := NewRegistry()
	registry.Register(fakeChannel{id: "x"})
	registry.Register(fakeChannel{id: "x"})
	if len(registry.All()) != 1 {
		t.Fatalf("override kept %d channels", len(registry.All()))
	}
}

func TestRegistryOnInbound(t *testing.T) {
	registry := NewRegistry()
	received := make(chan InboundEnvelope, 1)
	registry.SetOnInbound(func(env InboundEnvelope) {
		received <- env
	})
	env := InboundEnvelope{ChannelID: "telegram", Text: "hi"}
	registry.OnInbound(env)
	select {
	case got := <-received:
		if got.Text != "hi" || got.ChannelID != "telegram" {
			t.Fatalf("onInbound got %#v", got)
		}
	default:
		t.Fatal("callback was not invoked")
	}

	noCallback := NewRegistry()
	noCallback.OnInbound(env)
}

func TestRegistryConcurrentAccess(t *testing.T) {
	registry := NewRegistry()
	registry.SetOnInbound(func(InboundEnvelope) {})

	const workers = 16
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			id := fmt.Sprintf("chan-%d", n)
			registry.Register(fakeChannel{id: id})
			_, _ = registry.Get(id)
			registry.OnInbound(InboundEnvelope{ChannelID: id})
			registry.All()
		}(i)
	}
	wg.Wait()

	if len(registry.All()) != workers {
		t.Fatalf("registered = %d, want %d", len(registry.All()), workers)
	}
}