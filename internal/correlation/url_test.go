package correlation

import "testing"

func TestURLPolicyRejectsUnsafeURLsAndAllowsLoopbackHTTP(t *testing.T) {
	policy := URLPolicy{}
	tests := []struct {
		name, raw string
		want      string
	}{
		{name: "loopback", raw: "http://127.0.0.1:3000", want: "http://127.0.0.1:3000"},
		{name: "credentials", raw: "http://user:secret@127.0.0.1:3000"},
		{name: "control character", raw: "http://127.0.0.1:3000/\nrun"},
		{name: "remote host", raw: "https://example.com:3000"},
		{name: "non http", raw: "file:///tmp/a"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := policy.Validate(tt.raw)
			if tt.want == "" && err == nil {
				t.Fatalf("Validate(%q) succeeded", tt.raw)
			}
			if tt.want != "" && (err != nil || got != tt.want) {
				t.Fatalf("Validate(%q) = %q, %v; want %q", tt.raw, got, err, tt.want)
			}
		})
	}
}
