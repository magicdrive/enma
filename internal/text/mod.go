package text

import _ "embed"

//go:embed help.txt
var HelpMessage string

//go:embed default_config_toml/hotload.enma.toml
var DefaultHotloadEnmaToml string

//go:embed default_config_toml/watch.enma.toml
var DefaultWatchEnmaToml string
