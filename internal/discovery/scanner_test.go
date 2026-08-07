package discovery

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

type fakeRunner struct {
	argv []string
	out  []byte
	err  error
}

func (f *fakeRunner) Run(_ context.Context, argv ...string) ([]byte, error) {
	f.argv = append([]string(nil), argv...)
	return f.out, f.err
}

func TestDarwinScannerTargetsOnlySuppliedPIDs(t *testing.T) {
	for _, tt := range []struct {
		name    string
		pids    []int
		out     []byte
		err     error
		want    []Listener
		wantErr bool
	}{
		{
			name: "parses targeted listener with fixed argv",
			pids: []int{42}, out: []byte("p42\x00nTCP 127.0.0.1:3000 (LISTEN)\x00"),
			want: []Listener{{PID: 42, Address: "127.0.0.1", Port: 3000}},
		},
		{
			name: "parses Darwin lsof field output without display decorations",
			pids: []int{42}, out: []byte("p42\x00cpython\x00n127.0.0.1:3000\x00"),
			want: []Listener{{PID: 42, Address: "127.0.0.1", Port: 3000}},
		},
		{name: "rejects malformed targeted output", pids: []int{42}, out: []byte("pnot-a-pid\x00"), wantErr: true},
		{name: "propagates targeted lookup failure", pids: []int{42}, err: errors.New("lsof failed"), wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			runner := &fakeRunner{out: tt.out, err: tt.err}
			listeners, err := (DarwinScanner{Runner: runner}).ScanPIDs(context.Background(), tt.pids)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ScanPIDs() error = %v, wantErr %v", err, tt.wantErr)
			}
			if want := []string{"lsof", "-nP", "-a", "-p", "42", "-iTCP", "-sTCP:LISTEN", "-F0pcn"}; !reflect.DeepEqual(runner.argv, want) {
				t.Fatalf("argv = %#v, want %#v", runner.argv, want)
			}
			if !reflect.DeepEqual(listeners, tt.want) {
				t.Fatalf("listeners = %#v, want %#v", listeners, tt.want)
			}
		})
	}
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
	runner := &fakeRunner{out: []byte("42 1 42 /usr/bin/node node server.js\n")}
	table := DarwinProcessTable{Runner: runner}
	process, err := table.Lookup(context.Background(), 42)
	if err != nil {
		t.Fatalf("Lookup() error = %v", err)
	}
	if want := []string{"ps", "-o", "pid=,ppid=,pgid=,comm=,args=", "-p", "42"}; !reflect.DeepEqual(runner.argv, want) {
		t.Fatalf("argv = %#v, want %#v", runner.argv, want)
	}
	if process.PID != 42 || process.ParentPID != 1 || process.CWD != "" || process.Executable != "/usr/bin/node" {
		t.Fatalf("process = %#v", process)
	}
}
