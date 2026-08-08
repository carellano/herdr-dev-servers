package herdr

import "testing"

func TestCapabilitiesRequireProtocolAndSchema(t *testing.T) {
	for _, test := range []struct {
		name  string
		value Capabilities
		want  bool
	}{
		{name: "required capability", value: Capabilities{Protocol: 19, Schema: 1}, want: true},
		{name: "old protocol", value: Capabilities{Protocol: 18, Schema: 1}, want: false},
		{name: "different schema", value: Capabilities{Protocol: 19, Schema: 2}, want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := test.value.Compatible(); got != test.want {
				t.Fatalf("Compatible() = %t, want %t", got, test.want)
			}
		})
	}
}
