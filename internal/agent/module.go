package agent

import "go.uber.org/fx"

// Module provides the agent via fx dependency injection.
var Module = fx.Module("agent",
	fx.Provide(New),
)
