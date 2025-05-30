package textbank

import (
	_ "embed"
)

//go:embed start_message.txt
var BareStartMessage string

//go:embed short_help.txt
var ShortHelpMessage string

//go:embed help.txt
var HelpMessage string

//go:embed individual_help/enma_hotload_help.txt
var HotloadHelpMessage string

//go:embed individual_help/enma_watch_help.txt
var WatchHelpMessage string

//go:embed individual_help/enma_init_help.txt
var InitHelpMessage string

//go:embed config_toml/hotload.enma.toml
var DefaultHotloadEnmaToml string

//go:embed config_toml/watch.enma.toml
var DefaultWatchEnmaToml string

//go:embed enmaignore/maximum.enmaignore
var MaximumEnmaIgnore string

//go:embed enmaignore/minimal.enmaignore
var MinimalEnmaIgnore string
