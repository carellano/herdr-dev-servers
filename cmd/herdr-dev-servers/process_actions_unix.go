//go:build darwin || linux

package main

import (
	"github.com/carellano/herdr-dev-servers/internal/actions"
	"github.com/carellano/herdr-dev-servers/internal/adapter"
)

func productionProcessActions(factory adapter.Factory) (actions.ProcessInspector, actions.Signaler) {
	return adapter.NewProcessInspector(factory.Processes), actions.UnixSignaler{}
}
