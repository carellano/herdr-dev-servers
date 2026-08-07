// Package herdr defines conservative contracts for the unvalidated Herdr 0.8+ protocol.
package herdr

import "encoding/json"

const (
	RequiredProtocol = 19
	RequiredSchema   = 1
)

// Capabilities is the minimally required portion of a Herdr compatibility reply.
// Unknown fields remain in Raw so newer servers do not fail decoding by default.
type Capabilities struct {
	Protocol int             `json:"protocol"`
	Schema   int             `json:"schema"`
	Raw      json.RawMessage `json:"-"`
}

// Compatible reports only the protocol/schema gate inferred from static 0.8 evidence.
func (c Capabilities) Compatible() bool {
	return c.Protocol >= RequiredProtocol && c.Schema == RequiredSchema
}
