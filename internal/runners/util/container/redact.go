package container

import "strings"

const redactedEnvValue = "REDACTED"

// RedactEnvArgs returns a copy of container runner arguments with explicit
// environment variable values removed. The original arguments are left
// untouched so callers can safely use the result for logging and the original
// slice for execution.
func RedactEnvArgs(args []string) []string {
	redacted := append([]string(nil), args...)

	for i := 0; i < len(redacted); i++ {
		switch redacted[i] {
		case "-e", "--env":
			if i+1 < len(redacted) {
				redacted[i+1] = redactEnvSpec(redacted[i+1])
				i++
			}
		default:
			for _, prefix := range []string{"-e=", "--env="} {
				if spec, ok := strings.CutPrefix(redacted[i], prefix); ok {
					redacted[i] = prefix + redactEnvSpec(spec)
					break
				}
			}
		}
	}

	return redacted
}

func redactEnvSpec(spec string) string {
	name, _, ok := strings.Cut(spec, "=")
	if !ok {
		return spec
	}

	return name + "=" + redactedEnvValue
}
