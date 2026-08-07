package discovery

import (
	"context"
	"reflect"
	"testing"
)

type fakeRunner struct {
	argv []string
	out  []byte
}

func (f *fakeRunner) Run(_ context.Context, argv ...string) ([]byte, error) {
	f.argv = append([]string(nil), argv...)
	return f.out, nil
}

func TestDarwinScannerUsesFixedArgvAndParsesListeners(t *testing.T) {
	runner := &fakeRunner{out: []byte("p42\x00cnode\x00nTCP *:3000 (LISTEN)\x00p42\x00cnode\x00nTCP 127.0.0.1:5173 (LISTEN)\x00")}
	scanner := DarwinScanner{Runner: runner}

	listeners, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if want := []string{"lsof", "-nP", "-iTCP", "-sTCP:LISTEN", "-F0pcn"}; !reflect.DeepEqual(runner.argv, want) {
		t.Fatalf("argv = %#v, want %#v", runner.argv, want)
	}
	if want := []Listener{{PID: 42, Port: 3000, Address: "*"}, {PID: 42, Port: 5173, Address: "127.0.0.1"}}; !reflect.DeepEqual(listeners, want) {
		t.Fatalf("listeners = %#v, want %#v", listeners, want)
	}
}

func TestParseLinuxTCPListenersUsesDeterministicFixture(t *testing.T) {
	fixture := "  0: 0100007F:0BB8 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        42 12345 1 0000000000000000 100 0 0 10 0\n"
	listeners, err := ParseLinuxTCPListeners([]byte(fixture), map[string]int{"12345": 77})
	if err != nil {
		t.Fatalf("ParseLinuxTCPListeners() error = %v", err)
	}
	if want := []Listener{{PID: 77, Port: 3000, Address: "127.0.0.1"}}; !reflect.DeepEqual(listeners, want) {
		t.Fatalf("listeners = %#v, want %#v", listeners, want)
	}
}

func TestDarwinProcessTableUsesFixedArgv(t *testing.T) {
	runner := &fakeRunner{out: []byte("42 1 42 /work/app /usr/bin/node node server.js\n")}
	table := DarwinProcessTable{Runner: runner}
	process, err := table.Lookup(context.Background(), 42)
	if err != nil {
		t.Fatalf("Lookup() error = %v", err)
	}
	if want := []string{"ps", "-o", "pid=,ppid=,pgid=,cwd=,comm=,args=", "-p", "42"}; !reflect.DeepEqual(runner.argv, want) {
		t.Fatalf("argv = %#v, want %#v", runner.argv, want)
	}
	if process.PID != 42 || process.ParentPID != 1 || process.CWD != "/work/app" || process.Executable != "/usr/bin/node" {
		t.Fatalf("process = %#v", process)
	}
}
