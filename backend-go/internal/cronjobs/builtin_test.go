package cronjobs

import (
	"context"
	"testing"
)

func TestBuiltinTaskRegistrationFollowsEnabledFlag(t *testing.T) {
	service := newCronService(t)

	enabled := true
	var runs int
	service.RegisterBuiltins(BuiltinTask{
		Name:     "docker-auto-update",
		Enabled:  func() bool { return enabled },
		Schedule: func() string { return "0 4 * * *" },
		Run:      func(context.Context) { runs++ },
	})

	if len(service.builtinEntries) != 1 {
		t.Fatalf("builtin should be scheduled, got %d", len(service.builtinEntries))
	}

	enabled = false
	if err := service.ReloadBuiltins(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(service.builtinEntries) != 0 {
		t.Fatalf("disabled builtin should be removed, got %d", len(service.builtinEntries))
	}

	enabled = true
	if err := service.ReloadAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(service.builtinEntries) != 1 {
		t.Fatalf("ReloadAll should re-add enabled builtin, got %d", len(service.builtinEntries))
	}
	if runs != 0 {
		t.Fatalf("builtin should not run on registration, got %d", runs)
	}
}

func TestBuiltinTaskInvalidScheduleIsSkipped(t *testing.T) {
	service := newCronService(t)
	service.RegisterBuiltins(BuiltinTask{
		Name:     "broken",
		Enabled:  func() bool { return true },
		Schedule: func() string { return "not a cron" },
		Run:      func(context.Context) {},
	})
	if len(service.builtinEntries) != 0 {
		t.Fatalf("invalid schedule must not be scheduled, got %d", len(service.builtinEntries))
	}
}
