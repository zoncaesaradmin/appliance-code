// Package version holds build-time identity for the control plane binary.
package version

import "runtime"

// These are overridden at build time via:
//
//	go build -ldflags "-X appliance-code/services/controlplane/internal/version.Version=... -X appliance-code/services/controlplane/internal/version.Commit=... -X appliance-code/services/controlplane/internal/version.BuildTime=..."
var (
	Version   = "0.0.0-dev"
	Commit    = "unknown"
	BuildTime = "unknown"
)

// Info is the reportable version snapshot for internal operator endpoints.
type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildTime string `json:"buildTime"`
	GoVersion string `json:"goVersion"`
}

// Current returns the version snapshot for this running process.
func Current() Info {
	return Info{
		Version:   Version,
		Commit:    Commit,
		BuildTime: BuildTime,
		GoVersion: runtime.Version(),
	}
}
