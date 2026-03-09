package guidelines

import "go.uber.org/fx"

// Module provides guidelines management.
var Module = fx.Module("guidelines",
	fx.Provide(NewManager),
)
