package version

var (
	Version = "dev"
	Commit  = ""
	Date    = ""
)

func String() string {
	if Commit == "" {
		return Version
	}

	return Version + " (" + Commit + ")"
}
