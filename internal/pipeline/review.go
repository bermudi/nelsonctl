package pipeline

import "strings"

// lastNonEmptyLine returns the last non-blank line from s, lowercased and trimmed.
func lastNonEmptyLine(s string) string {
	lines := strings.Split(s, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line != "" {
			return strings.ToLower(line)
		}
	}
	return ""
}

// ReviewPassed determines whether review output should be considered a pass.
// Ambiguous results are treated as passes.
//
// Review tools can signal pass/fail unambiguously by emitting a structured
// marker as the last non-empty line of output ("pass", "lgtm", "approved",
// "fail", "rejected", "blocked"). When no marker is found, heuristic
// substring checks are used as a fallback.
func ReviewPassed(output string, exitCode int) bool {
	if exitCode != 0 {
		return false
	}

	// Check the last non-empty line for explicit structured markers.
	if marker := lastNonEmptyLine(output); marker != "" {
		switch marker {
		case "pass", "lgtm", "approved":
			return true
		case "fail", "rejected", "blocked":
			return false
		}
	}

	lower := strings.ToLower(output)
	if strings.Contains(lower, "failed") || strings.Contains(lower, "failure") || strings.Contains(lower, "failing") || strings.Contains(lower, "fail ") || strings.HasPrefix(lower, "fail") {
		return false
	}
	if strings.Contains(lower, "needs work") || strings.Contains(lower, "must fix") || strings.Contains(lower, "blocking") || strings.Contains(lower, "cannot approve") {
		return false
	}
	if strings.Contains(lower, "issues found") && !strings.Contains(lower, "no issues found") {
		return false
	}
	if strings.Contains(lower, "no issues found") {
		return true
	}
	return true
}

// ReviewOutput combines stdout/stderr text for detection.
func ReviewOutput(stdout, stderr string) string {
	stdout = strings.TrimSpace(stdout)
	stderr = strings.TrimSpace(stderr)
	if stdout == "" {
		return stderr
	}
	if stderr == "" {
		return stdout
	}
	return stdout + "\n" + stderr
}
