package secrets

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
)

var ErrNotFound = errors.New("secret not found")

// Key identifies a secret without carrying the secret value itself.
type Key struct {
	TraderID  string
	SessionID string
	Provider  string
	Name      string
}

// Normalize returns a copy with whitespace removed and provider/name lower-cased.
func (k Key) Normalize() Key {
	return Key{
		TraderID:  strings.TrimSpace(k.TraderID),
		SessionID: strings.TrimSpace(k.SessionID),
		Provider:  strings.ToLower(strings.TrimSpace(k.Provider)),
		Name:      strings.ToLower(strings.TrimSpace(k.Name)),
	}
}

func (k Key) String() string {
	k = k.Normalize()
	parts := []string{
		"provider=" + valueOrWildcard(k.Provider),
		"name=" + valueOrWildcard(k.Name),
	}
	if k.TraderID != "" {
		parts = append(parts, "trader="+k.TraderID)
	}
	if k.SessionID != "" {
		parts = append(parts, "session="+k.SessionID)
	}
	return strings.Join(parts, " ")
}

func (k Key) validate() error {
	k = k.Normalize()
	if k.Provider == "" {
		return fmt.Errorf("secrets: provider is required")
	}
	if k.Name == "" {
		return fmt.Errorf("secrets: name is required")
	}
	return nil
}

// Secret carries the resolved value. Do not log this struct with %+v.
type Secret struct {
	Key    Key
	Value  string
	Source string
}

// SecretStore resolves scoped secrets for a trader/session/provider/name tuple.
type SecretStore interface {
	Lookup(ctx context.Context, key Key) (Secret, error)
}

type EnvStore struct {
	Prefix string
}

func NewEnvStore() *EnvStore {
	return &EnvStore{Prefix: "ASTRAQUANT"}
}

func (s *EnvStore) Lookup(ctx context.Context, key Key) (Secret, error) {
	if err := ctx.Err(); err != nil {
		return Secret{}, err
	}
	key = key.Normalize()
	if err := key.validate(); err != nil {
		return Secret{}, err
	}
	for _, envName := range s.envNames(key) {
		if value, ok := os.LookupEnv(envName); ok && strings.TrimSpace(value) != "" {
			return Secret{Key: key, Value: strings.TrimSpace(value), Source: envName}, nil
		}
	}
	return Secret{}, fmt.Errorf("secrets: %w: %s", ErrNotFound, key.String())
}

func (s *EnvStore) envNames(key Key) []string {
	prefix := strings.ToUpper(strings.TrimSpace(s.Prefix))
	if prefix == "" {
		prefix = "ASTRAQUANT"
	}
	provider := envToken(key.Provider)
	name := envToken(key.Name)
	trader := envToken(key.TraderID)
	session := envToken(key.SessionID)

	var names []string
	if trader != "" && session != "" {
		names = append(names, prefix+"_SECRET_"+trader+"_"+session+"_"+provider+"_"+name)
	}
	if trader != "" {
		names = append(names, prefix+"_SECRET_"+trader+"_"+provider+"_"+name)
	}
	if session != "" {
		names = append(names, prefix+"_SECRET_"+session+"_"+provider+"_"+name)
	}
	names = append(names,
		prefix+"_SECRET_"+provider+"_"+name,
		prefix+"_"+provider+"_"+name,
	)
	if provider == "HYPERLIQUID" || provider == "hyperliquid" {
		names = append(names, "HYPERLIQUID_"+name)
	}
	return dedupe(names)
}

var envRefPattern = regexp.MustCompile(`^\$\{?([A-Za-z_][A-Za-z0-9_]*)\}?$`)

func LookupEnvReference(raw string) (Secret, bool) {
	raw = strings.TrimSpace(raw)
	matches := envRefPattern.FindStringSubmatch(raw)
	if len(matches) != 2 {
		return Secret{}, false
	}
	envName := matches[1]
	value, ok := os.LookupEnv(envName)
	if !ok || strings.TrimSpace(value) == "" {
		return Secret{}, true
	}
	return Secret{Value: strings.TrimSpace(value), Source: envName}, true
}

func IsEnvReference(raw string) bool {
	return envRefPattern.MatchString(strings.TrimSpace(raw))
}

func envToken(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	var b strings.Builder
	lastUnderscore := false
	for _, r := range value {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	return strings.Trim(b.String(), "_")
}

func dedupe(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func valueOrWildcard(value string) string {
	if value == "" {
		return "*"
	}
	return value
}
