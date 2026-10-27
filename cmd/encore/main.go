package main

import (
	"os"
	// Time zone names (kodi[].timezone, the settings time zone) resolve even in a container without tzdata.
	_ "time/tzdata"

	"github.com/nightah/encore/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
