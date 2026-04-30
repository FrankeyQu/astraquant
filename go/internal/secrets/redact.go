package secrets

import (
	"fmt"
	"strings"
)

const redacted = "[REDACTED]"

func Redact(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if len(value) <= 8 {
		return redacted
	}
	return value[:4] + redacted + value[len(value)-4:]
}

func RedactText(text string, sensitiveValues ...string) string {
	out := text
	for _, value := range sensitiveValues {
		value = strings.TrimSpace(value)
		if len(value) < 4 {
			continue
		}
		out = strings.ReplaceAll(out, value, Redact(value))
	}
	return out
}

func RedactError(err error, sensitiveValues ...string) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s", RedactText(err.Error(), sensitiveValues...))
}
