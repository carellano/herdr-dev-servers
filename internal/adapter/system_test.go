package adapter

import (
	"context"
	"errors"
	"io"
	"reflect"
	"testing"
	"time"

	"github.com/carellano/herdr-dev-servers/internal/actions"
)

type fakeSystemCommand struct {
	started, ran, released, discarded bool
	stdin                             string
	startErr, runErr, releaseErr      error
	run                               func() error
}

func (c *fakeSystemCommand) Start() error { c.started = true; return c.startErr }
func (c *fakeSystemCommand) Run() error {
	c.ran = true
	if c.run != nil {
		return c.run()
	}
	return c.runErr
}
func (c *fakeSystemCommand) Release() error       { c.released = true; return c.releaseErr }
func (c *fakeSystemCommand) SetStdin(r io.Reader) { data, _ := io.ReadAll(r); c.stdin = string(data) }
func (c *fakeSystemCommand) DiscardStdio()        { c.discarded = true }

func TestSystemRunner(t *testing.T) {
	for _, test := range []struct {
		name             string
		ctx              context.Context
		command          actions.Command
		startErr, runErr error
		want             string
		detach           bool
	}{
		{"detaches", deadlineContext(t), actions.Command{Path: "open", Args: []string{"http://127.0.0.1"}, Detach: true}, nil, nil, "", true},
		{"synchronous failure", context.Background(), actions.Command{Path: "tool"}, nil, errors.New("failed"), "failed", false},
		{"canceled", canceledContext(), actions.Command{Path: "open"}, nil, nil, context.Canceled.Error(), false},
		{"invalid arg", context.Background(), actions.Command{Path: "open", Args: []string{"bad\n"}}, nil, nil, "invalid command argument", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := &fakeSystemCommand{startErr: test.startErr, runErr: test.runErr}
			var gotCtx context.Context
			var path string
			var args []string
			runner := SystemRunner{Command: func(ctx context.Context, p string, a ...string) systemCommand {
				gotCtx, path, args = ctx, p, a
				return fake
			}}
			err := runner.Start(test.ctx, test.command)
			if errorText(err) != test.want {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
			if test.want == "" && (!fake.discarded || fake.started != test.detach || fake.ran == test.detach || fake.released != test.detach) {
				t.Fatalf("command = %#v", fake)
			}
			if test.want == "" && (path != test.command.Path || !reflect.DeepEqual(args, test.command.Args)) {
				t.Fatalf("command = %q %#v", path, args)
			}
			if test.detach && gotCtx.Done() != nil {
				t.Fatal("detached command retained cancellation")
			}
		})
	}
}

func TestSystemClipboard(t *testing.T) {
	for _, test := range []struct {
		name, platform, wayland string
		tools                   map[string]string
		wantPath                string
		wantArgs                []string
		want                    string
	}{
		{"darwin", "darwin", "", nil, "pbcopy", nil, ""},
		{"wayland", "linux", "wayland-1", map[string]string{"wl-copy": "/bin/wl-copy"}, "/bin/wl-copy", nil, ""},
		{"xclip fallback", "linux", "wayland-1", map[string]string{"xclip": "/bin/xclip"}, "/bin/xclip", []string{"-selection", "clipboard"}, ""},
		{"xsel fallback", "linux", "", map[string]string{"xsel": "/bin/xsel"}, "/bin/xsel", []string{"--clipboard", "--input"}, ""},
		{"missing", "linux", "", nil, "", nil, "system clipboard command is unavailable"},
		{"unsupported", "windows", "", nil, "", nil, "clipboard is unsupported on \"windows\""},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := &fakeSystemCommand{}
			var path string
			var args []string
			var bounded bool
			clipboard := SystemClipboard{Platform: test.platform, Timeout: time.Second, Getenv: func(string) string { return test.wayland }, LookPath: func(name string) (string, error) {
				if path := test.tools[name]; path != "" {
					return path, nil
				}
				return "", errors.New("missing")
			}, Command: func(ctx context.Context, p string, a ...string) systemCommand {
				_, bounded = ctx.Deadline()
				path, args = p, a
				return fake
			}}
			err := clipboard.WriteText("http://127.0.0.1:3000")
			if errorText(err) != test.want || path != test.wantPath || !reflect.DeepEqual(args, test.wantArgs) {
				t.Fatalf("error=%v command=%q %#v", err, path, args)
			}
			if test.want == "" && (!fake.ran || !fake.discarded || !bounded || fake.stdin != "http://127.0.0.1:3000") {
				t.Fatalf("command = %#v", fake)
			}
		})
	}
}

func TestSystemClipboardTimesOut(t *testing.T) {
	fake := &fakeSystemCommand{}
	clipboard := SystemClipboard{Platform: "darwin", Timeout: time.Millisecond, Command: func(ctx context.Context, _ string, _ ...string) systemCommand {
		fake.run = func() error { <-ctx.Done(); return ctx.Err() }
		return fake
	}}
	if err := clipboard.WriteText("http://127.0.0.1"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want deadline exceeded", err)
	}
}

func canceledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}
func deadlineContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	t.Cleanup(cancel)
	return ctx
}
func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
