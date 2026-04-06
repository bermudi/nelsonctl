package version

import (
	"runtime/debug"
	"strings"
)

var Version string = "dev"

func Get() string {
	if Version != "dev" {
		return Version
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return Version
	}
	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" {
			hash := setting.Value
			if len(hash) >= 7 {
				hash = hash[:7]
			}
			var v string
			if info.Main.Version != "" {
				v = info.Main.Version
			} else {
				v = "dev"
			}
			return strings.TrimPrefix(v, "v") + "+" + hash
		}
	}
	return Version
}
