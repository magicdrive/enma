package text

import _ "embed"

//go:embed help.txt
var HelpMessage string

//go:embed config_toml/hotload.enma.toml
var DefaultHotloadEnmaToml string

//go:embed config_toml/watch.enma.toml
var DefaultWatchEnmaToml string

//go:embed enmaignore/sample.enmaignore
var DefaultEnmaIgnore string
