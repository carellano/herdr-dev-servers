//go:build darwin || linux

package main

import (
	"github.com/carellano/herdr-apps/internal/actions"
	"github.com/carellano/herdr-apps/internal/adapter"
)

func productionProcessActions(factory adapter.Factory) (actions.ProcessInspector, actions.Signaler) {
	return adapter.NewProcessInspector(factory.Processes), actions.UnixSignaler{}
}
