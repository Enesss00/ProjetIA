package sim

// jobDuration returns how long (in ticks) a player job of the given kind
// takes to complete. Scans and forensics take real game time so the player
// must spend their attention wisely.
func jobDuration(kind string) int {
	switch kind {
	case "scan-net":
		return 5 * TicksPerSecond
	case "scan-host":
		return 2 * TicksPerSecond
	case "patch":
		return 4 * TicksPerSecond
	case "restart":
		return 3 * TicksPerSecond
	case "forensics":
		return 3 * TicksPerSecond
	default:
		return 1
	}
}
