package main

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/carellano/herdr-apps/internal/actions"
	"github.com/carellano/herdr-apps/internal/adapter"
	"github.com/carellano/herdr-apps/internal/config"
)

func TestRunRoutesEnsureWatch(t *testing.T) {
	called := false
	err := runWithEnsure([]string{"ensure-watch"}, &bytes.Buffer{}, func(context.Context) error {
		called = true
		return nil
	})
	if err != nil || !called {
		t.Fatalf("error = %v, called = %v", err, called)
	}
}

func TestNewDaemonServiceUsesFactoryFocusClient(t *testing.T) {
	focus := &mainFakeFocus{}
	service := newDaemonService(adapter.Factory{Focus: focus}, config.Defaults())
	executor, ok := service.Actions.(*actions.Executor)
	if !ok || executor.Service.Focus != focus {
		t.Fatalf("actions = %#v, want factory focus client", service.Actions)
	}
}

func TestNewDaemonServiceHonorsSystemActions(t *testing.T) {
	focus := &mainFakeFocus{}
	for _, test := range []struct {
		name       string
		cfg        config.Config
		open, copy bool
	}{
		{"system", config.Defaults(), true, true},
		{"disabled", config.Config{Opener: "disabled", Clipboard: "disabled"}, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			executor := newDaemonService(adapter.Factory{Focus: focus}, test.cfg).Actions.(*actions.Executor)
			if (executor.Service.Runner != nil) != test.open || (executor.Service.Clipboard != nil) != test.copy || executor.Service.Focus != focus {
				t.Fatalf("service = %#v", executor.Service)
			}
		})
	}
}

func TestNewDaemonServiceWiresProductionProcessActions(t *testing.T) {
	executor := newDaemonService(adapter.Factory{}, config.Defaults()).Actions.(*actions.Executor)
	if _, ok := executor.Service.Processes.(adapter.ProcessInspector); !ok {
		t.Fatalf("process inspector = %T, want adapter.ProcessInspector", executor.Service.Processes)
	}
	if _, ok := executor.Service.Signaler.(actions.UnixSignaler); !ok {
		t.Fatalf("signaler = %T, want actions.UnixSignaler", executor.Service.Signaler)
	}
	if executor.Service.Processes == nil || executor.Service.Signaler == nil {
		t.Fatalf("service = %#v, want production process boundaries", executor.Service)
	}
}

type mainFakeFocus struct{}

func (*mainFakeFocus) FocusPane(string, string, string) error { return nil }
func (*mainFakeFocus) FocusWorkspaceTab(string, string) error { return nil }
func (*mainFakeFocus) CurrentFocus() (actions.FocusLocation, error) {
	return actions.FocusLocation{}, nil
}

func TestRunOpensManifestPaneWithLiteralArguments(t *testing.T) {
	var gotPath string
	var gotArgs []string
	err := runWith([]string{"open"}, &bytes.Buffer{}, nil, func(string) string { return "/usr/local/bin/herdr" }, func(_ context.Context, path string, args ...string) error {
		gotPath, gotArgs = path, args
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/usr/local/bin/herdr" || !reflect.DeepEqual(gotArgs, []string{"plugin", "pane", "open", "--plugin", "carellano.apps", "--entrypoint", "apps"}) {
		t.Fatalf("command = %q %#v", gotPath, gotArgs)
	}
}

func TestRunOpenRejectsUnsafePathAndRoutesCommandError(t *testing.T) {
	for _, test := range []struct {
		name string
		path string
		run  commandRunner
		want string
	}{
		{name: "missing", want: "HERDR_BIN_PATH is required"},
		{name: "relative", path: "herdr", want: "HERDR_BIN_PATH is unsafe"},
		{name: "control", path: "/usr/bin/herdr\n", want: "HERDR_BIN_PATH is unsafe"},
		{name: "runner", path: "/usr/bin/herdr", run: func(context.Context, string, ...string) error { return errors.New("denied") }, want: "open apps pane: denied"},
	} {
		t.Run(test.name, func(t *testing.T) {
			run := test.run
			if run == nil {
				run = func(context.Context, string, ...string) error { t.Fatal("runner called"); return nil }
			}
			err := runWith([]string{"open"}, &bytes.Buffer{}, nil, func(string) string { return test.path }, run)
			if err == nil || err.Error() != "open apps: "+test.want && err.Error() != test.want {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}
