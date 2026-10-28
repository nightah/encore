package buildinfo

import (
	"fmt"
	"runtime"
	"runtime/debug"
)

// Values overridden at link time by goreleaser and the Dockerfile.
var (
	Version = "dev"
	Commit  = ""
	Date    = ""
)

// readBuildInfo is debug.ReadBuildInfo, replaced in tests because test binaries carry no VCS stamp.
var readBuildInfo = debug.ReadBuildInfo

// Get returns the build metadata, falling back to the VCS stamp embedded by the Go toolchain when the binary was built without ldflags.
func Get() Info {
	info := Info{Version: Version, Commit: Commit, Date: Date, GoVersion: runtime.Version()}

	if info.Commit != "" {
		return info
	}

	if bi, ok := readBuildInfo(); ok {
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				info.Commit = s.Value
			case "vcs.time":
				info.Date = s.Value
			}
		}
	}

	return info
}

// String renders the build metadata on one line.
func (i Info) String() string {
	commit := i.Commit
	if len(commit) > 12 {
		commit = commit[:12]
	}

	if commit == "" {
		return fmt.Sprintf("encore %s (%s)", i.Version, i.GoVersion)
	}

	return fmt.Sprintf("encore %s (%s, %s)", i.Version, commit, i.GoVersion)
}
