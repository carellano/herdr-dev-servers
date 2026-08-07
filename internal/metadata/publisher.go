// Package metadata creates bounded workspace metadata without redundant writes.
package metadata

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
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
type Publisher struct {
	digest string
	values map[string]string
}

// Reporter is the exact workspace.report_metadata transport boundary.
type Reporter interface {
	ReportMetadata(context.Context, string, string, map[string]*string) error
}

// HerdrReporter adapts the exact workspace.report_metadata request without exposing socket details.
type HerdrReporter struct {
	Transport interface {
		ReportMetadata(context.Context, string, herdr.MetadataRequest) error
	}
	RequestID func() string
}

func (r HerdrReporter) ReportMetadata(ctx context.Context, workspace, source string, tokens map[string]*string) error {
	id := "herdr-apps-metadata"
	if r.RequestID != nil {
		id = r.RequestID()
	}
	return r.Transport.ReportMetadata(ctx, id, herdr.MetadataRequest{WorkspaceID: workspace, Source: source, Tokens: tokens})
}

// ReportError exposes a failed workspace metadata write without advancing local state.
type ReportError struct {
	WorkspaceID string
	Err         error
}

func (e *ReportError) Error() string {
	return "report workspace metadata " + e.WorkspaceID + ": " + e.Err.Error()
}
func (e *ReportError) Unwrap() error { return e.Err }

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

// Publish writes stable bounded $ports values, suppressing unchanged workspaces and clearing removals.
func (p *Publisher) Publish(ctx context.Context, applications []model.Application, reporter Reporter) error {
	if reporter == nil {
		return &ReportError{Err: fmt.Errorf("metadata reporter is unavailable")}
	}
	byWorkspace := map[string][]model.Application{}
	for _, app := range applications {
		if id := app.Association.WorkspaceID; id != "" {
			byWorkspace[id] = append(byWorkspace[id], app)
		}
	}
	values := make(map[string]string, len(byWorkspace))
	for id, apps := range byWorkspace {
		values[id] = boundedValue(apps)
	}
	previous := p.values
	for id := range previous {
		if _, ok := values[id]; !ok {
			values[id] = ""
		}
	}
	ids := make([]string, 0, len(values))
	for id := range values {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if previous[id] == values[id] {
			continue
		}
		value := values[id]
		if err := reporter.ReportMetadata(ctx, id, "herdr-apps", map[string]*string{"ports": &value}); err != nil {
			return &ReportError{WorkspaceID: id, Err: err}
		}
	}
	p.values = make(map[string]string, len(byWorkspace))
	for id, value := range values {
		if value != "" {
			p.values[id] = value
		}
	}
	return nil
}

func boundedValue(applications []model.Application) string {
	p := Publisher{}
	return p.Prepare(applications).Value
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
