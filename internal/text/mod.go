package text

import _ "embed"

//go:embed start_message.txt
var StartMessage string

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

//go:embed enmaignore/sample.enmaignore
var DefaultEnmaIgnore string
