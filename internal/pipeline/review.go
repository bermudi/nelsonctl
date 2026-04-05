package pipeline

import "strings"

// ReviewPassed determines whether review output should be considered a pass.
// Ambiguous results are treated as passes.
func ReviewPassed(output string, exitCode int) bool {
	if exitCode != 0 {
		return false
	}

	lower := strings.ToLower(output)
	if strings.Contains(lower, "no issues found") {
		return true
	}
	if strings.Contains(lower, "issues found") {
		return false
	}
	if strings.Contains(lower, "failed") || strings.Contains(lower, "failure") || strings.Contains(lower, "failing") || strings.Contains(lower, "fail ") || strings.HasPrefix(lower, "fail") {
		return false
	}
	if strings.Contains(lower, "needs work") || strings.Contains(lower, "must fix") || strings.Contains(lower, "blocking") || strings.Contains(lower, "cannot approve") {
		return false
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
