// Package correlation converts typed OS and Herdr observations into applications.
package correlation

import (
	"fmt"
	"sort"
	"time"

	"github.com/carellano/herdr-apps/internal/discovery"
	"github.com/carellano/herdr-apps/internal/model"
)

// PaneEvidence is the relevant, conservative portion of Herdr pane process evidence.
type PaneEvidence struct {
	WorkspaceID string
	TabID       string
	PaneID      string
	PID         int
	CWD         string
	ObservedAt  time.Time
}

// Input holds one complete discovery/correlation pass.
type Input struct {
	Listeners          []discovery.Listener
	Processes          map[int]discovery.Process
	Panes              []PaneEvidence
	ProcessUnavailable string
	ObservedAt         time.Time
}

// Result separates applications visible by default from external applications shown only in an explicit all view.
type Result struct {
	Applications []model.Application
	External     []model.Application
}

// All returns a stable complete view when the explicit all view is selected.
func (r Result) All() []model.Application {
	applications := append([]model.Application(nil), r.Applications...)
	applications = append(applications, r.External...)
	sortApplications(applications)
	return applications
}

// Build groups ports by stable logical identity and attaches only evidence that is actually present.
func Build(input Input) Result {
	observedAt := input.ObservedAt
	if observedAt.IsZero() {
		observedAt = time.Now().UTC()
	}
	groups := map[string]*model.Application{}
	for _, listener := range input.Listeners {
		process, exists := input.Processes[listener.PID]
		if !exists {
			process = discovery.Process{PID: listener.PID, StartTime: "unavailable"}
		}
		identity := LogicalIdentityWithAncestry(process, input.Processes)
		groupKey := fmt.Sprintf("%s:%d:%s", identity.Key, identity.PID, identity.StartTime)
		app := groups[groupKey]
		if app == nil {
			evidence := model.Evidence{Source: "discovery", ObservedAt: observedAt, Fresh: exists}
			if !exists {
				evidence.Unavailable = "process details unavailable"
			}
			app = &model.Application{ID: groupKey, Identity: identity, Association: model.Association{Confidence: model.ConfidenceUnknown}, Evidence: []model.Evidence{evidence}}
			groups[groupKey] = app
		}
		app.Endpoints = append(app.Endpoints, model.Endpoint{Port: listener.Port, URL: defaultURL(listener.Address, listener.Port)})
	}
	result := Result{}
	for _, app := range groups {
		associate(app, input.Processes[app.Identity.PID], input.Panes, observedAt)
		if input.ProcessUnavailable != "" {
			app.Evidence = append(app.Evidence, model.Evidence{Source: "process", ObservedAt: observedAt, Unavailable: input.ProcessUnavailable})
		}
		app.External = app.Association.Confidence == model.ConfidenceUnknown
		sort.Slice(app.Endpoints, func(i, j int) bool { return app.Endpoints[i].Port < app.Endpoints[j].Port })
		if app.External {
			result.External = append(result.External, *app)
		} else {
			result.Applications = append(result.Applications, *app)
		}
	}
	sortApplications(result.Applications)
	sortApplications(result.External)
	return result
}

func associate(app *model.Application, process discovery.Process, panes []PaneEvidence, observedAt time.Time) {
	var matches []PaneEvidence
	for _, pane := range panes {
		if pane.PID == app.Identity.PID || pane.PID == 0 && cwdMatches(process.CWD, pane.CWD) {
			matches = append(matches, pane)
		}
	}
	if len(matches) == 1 {
		pane := matches[0]
		app.Association = model.Association{WorkspaceID: pane.WorkspaceID, TabID: pane.TabID, PaneID: pane.PaneID, Confidence: model.ConfidenceHigh}
		app.Evidence = append(app.Evidence, model.Evidence{Source: "herdr-pane", ObservedAt: pane.ObservedAt, Fresh: !pane.ObservedAt.IsZero()})
		return
	}
	if len(matches) > 1 {
		app.Association = model.Association{Confidence: model.ConfidencePartial, Stale: true}
		app.Evidence = append(app.Evidence, model.Evidence{Source: "herdr-pane", ObservedAt: observedAt, Fresh: false, Unavailable: "ambiguous pane evidence"})
	}
}

func cwdMatches(processCWD, paneCWD string) bool {
	return processCWD != "" && paneCWD != "" && (pathContainsSegment(processCWD, paneCWD) || pathContainsSegment(paneCWD, processCWD))
}

func defaultURL(address string, port int) string {
	if address == "127.0.0.1" || address == "::1" || address == "localhost" {
		return fmt.Sprintf("http://%s:%d", address, port)
	}
	return ""
}

func sortApplications(applications []model.Application) {
	sort.Slice(applications, func(i, j int) bool { return applications[i].ID < applications[j].ID })
}
