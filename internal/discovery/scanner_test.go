package discovery

import (
	"context"
	"errors"
	"reflect"
	"strings"
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

func TestParseDarwinLsofAssignsNewlineDelimitedRecordsToTheirPIDs(t *testing.T) {
	data := []byte("p666\x00cnode\x00nTCP 127.0.0.1:3000 (LISTEN)\x00\np83195\x00\ncnode\x00\nnTCP 127.0.0.1:38186 (LISTEN)\x00\np83196\x00\ncnode\x00\nnTCP 127.0.0.1:38187 (LISTEN)\x00")

	listeners, err := ParseDarwinLsof(data)
	if err != nil {
		t.Fatalf("ParseDarwinLsof() error = %v", err)
	}
	if want := []Listener{
		{PID: 666, Port: 3000, Address: "127.0.0.1"},
		{PID: 83195, Port: 38186, Address: "127.0.0.1"},
		{PID: 83196, Port: 38187, Address: "127.0.0.1"},
	}; !reflect.DeepEqual(listeners, want) {
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

func TestMergeLinuxTCPListenersParsesIPv4AndIPv6(t *testing.T) {
	tcp := []byte("  0: 0100007F:0BB8 00000000:0000 0A 00000000:00000000 00:00000000 00000000 1000 42 1\n")
	tcp6 := []byte("  0: 00000000000000000000000001000000:0BB9 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000 1000 42 2\n  1: 00000000000000000000000000000000:0BBA 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000 1000 42 3\n")
	listeners, err := MergeLinuxTCPListeners(tcp, tcp6, map[string]int{"1": 7, "2": 8, "3": 9})
	if err != nil {
		t.Fatal(err)
	}
	want := []Listener{{PID: 7, Address: "127.0.0.1", Port: 3000}, {PID: 8, Address: "::1", Port: 3001}, {PID: 9, Address: "::", Port: 3002}}
	if !reflect.DeepEqual(listeners, want) {
		t.Fatalf("listeners=%#v want=%#v", listeners, want)
	}
}

func TestDarwinProcessTableUsesFixedArgv(t *testing.T) {
	runner := &fakeRunner{out: []byte("42 1 42 Thu Aug  7 12:34:56 2026 /usr/bin/node node server.js\n")}
	table := DarwinProcessTable{Runner: runner}
	process, err := table.Lookup(context.Background(), 42)
	if err != nil {
		t.Fatalf("Lookup() error = %v", err)
	}
	if want := []string{"ps", "-o", "pid=,ppid=,pgid=,lstart=,comm=,args=", "-p", "42"}; !reflect.DeepEqual(runner.argv, want) {
		t.Fatalf("argv = %#v, want %#v", runner.argv, want)
	}
	if process.PID != 42 || process.ParentPID != 1 || process.StartTime != "Thu Aug 7 12:34:56 2026" || process.CWD != "" || process.Executable != "/usr/bin/node" {
		t.Fatalf("process = %#v", process)
	}
}

func TestParseDarwinProcess(t *testing.T) {
	for _, test := range []struct {
		name string
		data string
		want Process
		bad  bool
	}{
		{name: "normal", data: "42 1 42 Thu Aug  7 12:34:56 2026 /usr/bin/node node server.js", want: Process{PID: 42, ParentPID: 1, PGID: 42, StartTime: "Thu Aug 7 12:34:56 2026", Executable: "/usr/bin/node", Args: []string{"node", "server.js"}}},
		{name: "spaces in arguments", data: "42 1 42 Thu Aug  7 12:34:56 2026 /bin/sh sh -c echo hello world", want: Process{PID: 42, ParentPID: 1, PGID: 42, StartTime: "Thu Aug 7 12:34:56 2026", Executable: "/bin/sh", Args: []string{"sh", "-c", "echo", "hello", "world"}}},
		{name: "malformed", data: "42 1", bad: true},
		{name: "invalid start", data: "42 1 42 impossible start time text /bin/sh sh", bad: true},
		{name: "missing command", data: "42 1 42 Thu Aug  7 12:34:56 2026", bad: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := ParseDarwinProcess([]byte(test.data))
			if (err != nil) != test.bad || !reflect.DeepEqual(got, test.want) {
				t.Fatalf("ParseDarwinProcess() = %#v, %v", got, err)
			}
		})
	}
}

func TestDarwinProcessTableRejectsPIDMismatch(t *testing.T) {
	_, err := (DarwinProcessTable{Runner: &fakeRunner{out: []byte("43 1 42 Thu Aug  7 12:34:56 2026 /bin/sh sh")}}).Lookup(context.Background(), 42)
	if err == nil {
		t.Fatal("Lookup() succeeded for mismatched PID")
	}
}

func TestParseLinuxProcessStat(t *testing.T) {
	stat := func(pid, command, parent, pgid, start string) []byte {
		fields := []string{"S", parent, pgid}
		for len(fields) < 19 {
			fields = append(fields, "0")
		}
		fields = append(fields, start)
		return []byte(pid + " (" + command + ") " + strings.Join(fields, " "))
	}
	for _, test := range []struct {
		name string
		pid  int
		data []byte
		want Process
		bad  bool
	}{
		{name: "ordinary command", pid: 42, data: stat("42", "node", "1", "42", "100"), want: Process{PID: 42, ParentPID: 1, PGID: 42, StartTime: "100"}},
		{name: "spaces in command", pid: 42, data: stat("42", "node worker", "1", "42", "100"), want: Process{PID: 42, ParentPID: 1, PGID: 42, StartTime: "100"}},
		{name: "closing parentheses in command", pid: 42, data: stat("42", "node) worker (dev", "1", "42", "100"), want: Process{PID: 42, ParentPID: 1, PGID: 42, StartTime: "100"}},
		{name: "missing closing parenthesis", pid: 42, data: []byte("42 (node S 1 42"), bad: true},
		{name: "incomplete fields", pid: 42, data: []byte("42 (node) S 1 42"), bad: true},
		{name: "invalid PID", pid: 42, data: stat("not-a-pid", "node", "1", "42", "100"), bad: true},
		{name: "mismatched PID", pid: 42, data: stat("43", "node", "1", "42", "100"), bad: true},
		{name: "invalid parent", pid: 42, data: stat("42", "node", "parent", "42", "100"), bad: true},
		{name: "invalid group", pid: 42, data: stat("42", "node", "1", "group", "100"), bad: true},
		{name: "invalid start", pid: 42, data: stat("42", "node", "1", "42", "start"), bad: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := ParseLinuxProcessStat(test.pid, test.data)
			if (err != nil) != test.bad || !reflect.DeepEqual(got, test.want) {
				t.Fatalf("ParseLinuxProcessStat() = %#v, %v", got, err)
			}
		})
	}
}
