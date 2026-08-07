// Package metadata creates bounded workspace metadata without redundant writes.
package metadata

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"

	"github.com/carellano/herdr-apps/internal/herdr"
	"github.com/carellano/herdr-apps/internal/model"
)

const (
	maxEndpoints = 6
	maxBytes     = 80
)

// Publication describes whether a metadata client should write the bounded $ports value.
type Publication struct {
	Value   string
	Digest  string
	Changed bool
}

// Publisher remembers the last digest so unchanged refreshes remain silent.
type Publisher struct{ digest string }

func (p *Publisher) Prepare(applications []model.Application) Publication {
	urls := make([]string, 0)
	for _, application := range applications {
		for _, endpoint := range application.Endpoints {
			if endpoint.URL != "" {
				urls = append(urls, endpoint.URL)
			}
		}
	}
	sort.Strings(urls)
	urls = compact(urls)
	remaining := 0
	if len(urls) > maxEndpoints {
		remaining = len(urls) - maxEndpoints
		urls = urls[:maxEndpoints]
	}
	value := strings.Join(urls, ", ")
	if remaining > 0 {
		value += " +" + strconvItoa(remaining)
	}
	for len([]byte(value)) > maxBytes && len(urls) > 0 {
		urls = urls[:len(urls)-1]
		remaining++
		value = strings.Join(urls, ", ") + " +" + strconvItoa(remaining)
	}
	sum := sha256.Sum256([]byte(value))
	digest := hex.EncodeToString(sum[:])
	changed := digest != p.digest
	p.digest = digest
	return Publication{Value: value, Digest: digest, Changed: changed}
}

func compact(values []string) []string {
	result := values[:0]
	for _, value := range values {
		if len(result) == 0 || result[len(result)-1] != value {
			result = append(result, value)
		}
	}
	return result
}
func strconvItoa(value int) string {
	if value == 0 {
		return "0"
	}
	digits := [20]byte{}
	index := len(digits)
	for value > 0 {
		index--
		digits[index] = byte('0' + value%10)
		value /= 10
	}
	return string(digits[index:])
}

// CompatibilityGuidance reports the static gate without claiming a live Herdr validation.
func CompatibilityGuidance(capabilities herdr.Capabilities) string {
	if capabilities.Compatible() {
		return "Herdr compatibility gate passed from reported capabilities; live validation remains required."
	}
	return "Herdr metadata unavailable: require protocol >= 19 and schema 1; update Herdr and retry."
}
