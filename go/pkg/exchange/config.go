package exchange

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"nof0-api/internal/secrets"
	"nof0-api/pkg/confkit"
)

// Config captures configuration for one or more exchange providers.
type Config struct {
	Default   string                     `yaml:"default"`
	Providers map[string]*ProviderConfig `yaml:"providers"`

	secretStore secrets.SecretStore
}

// ProviderConfig describes how to construct a specific exchange provider instance.
type ProviderConfig struct {
	Type         string `yaml:"type"`
	PrivateKey   string `yaml:"private_key"`
	APIKey       string `yaml:"api_key"`
	APISecret    string `yaml:"api_secret"`
	Passphrase   string `yaml:"passphrase"`
	VaultAddress string `yaml:"vault_address"`
	MainAddress  string `yaml:"main_address"` // Main account address (for API wallet scenarios)
	Testnet      bool   `yaml:"testnet"`

	Credentials CredentialScope `yaml:"credentials"`

	TimeoutRaw string        `yaml:"timeout"`
	Timeout    time.Duration `yaml:"-"`
}

// CredentialScope selects the trader/session/provider tuple used for secret lookup.
type CredentialScope struct {
	TraderID  string `yaml:"trader_id"`
	SessionID string `yaml:"session_id"`
	Provider  string `yaml:"provider"`
}

// ProviderBuilder constructs a Provider from configuration.
type ProviderBuilder func(name string, cfg *ProviderConfig) (Provider, error)

var (
	providerRegistry   = make(map[string]ProviderBuilder)
	providerRegistryMu sync.RWMutex
)

// RegisterProvider associates a builder with an exchange provider type.
func RegisterProvider(typeName string, builder ProviderBuilder) {
	providerRegistryMu.Lock()
	defer providerRegistryMu.Unlock()
	providerRegistry[strings.ToLower(strings.TrimSpace(typeName))] = builder
}

func lookupProviderBuilder(typeName string) (ProviderBuilder, bool) {
	providerRegistryMu.RLock()
	defer providerRegistryMu.RUnlock()
	builder, ok := providerRegistry[strings.ToLower(strings.TrimSpace(typeName))]
	return builder, ok
}

// GetProvider constructs a single provider instance for the given type using
// the provided configuration. This is a convenience for tests and callers that
// want to instantiate a provider without building a full config map.
func GetProvider(typeName string, cfg *ProviderConfig) (Provider, error) {
	if cfg == nil {
		cfg = &ProviderConfig{}
	}
	// Ensure the type is set and valid for validation.
	cfgCopy := *cfg
	cfgCopy.Type = typeName
	if err := cfgCopy.validate("inline"); err != nil {
		return nil, err
	}
	builder, ok := lookupProviderBuilder(cfgCopy.Type)
	if !ok {
		return nil, fmt.Errorf("exchange provider: unsupported type %q", cfgCopy.Type)
	}
	return builder("inline", &cfgCopy)
}

// LoadConfig reads configuration from disk.
func LoadConfig(path string) (*Config, error) {
	confkit.LoadDotenvOnce()
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open exchange config: %w", err)
	}
	defer file.Close()
	return LoadConfigFromReader(file)
}

// MustLoad reads configuration from the default project location and panics on error.
func MustLoad() *Config {
	path := confkit.MustProjectPath("etc/exchange.yaml")
	cfg, err := LoadConfig(path)
	if err != nil {
		panic(err)
	}
	return cfg
}

// LoadConfigFromReader constructs a Config from an io.Reader.
func LoadConfigFromReader(r io.Reader) (*Config, error) {
	confkit.LoadDotenvOnce()
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read exchange config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal exchange config: %w", err)
	}
	if err := cfg.normalise(); err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// SetSecretStore attaches a per-config secret store used while building providers.
func (c *Config) SetSecretStore(store secrets.SecretStore) {
	c.secretStore = store
}

func (c *Config) normalise() error {
	if c.Providers == nil {
		c.Providers = make(map[string]*ProviderConfig)
	}
	for name, provider := range c.Providers {
		if provider == nil {
			provider = &ProviderConfig{}
			c.Providers[name] = provider
		}
		provider.expandEnv()
		if err := provider.parseDurations(name); err != nil {
			return err
		}
	}
	return nil
}

func (p *ProviderConfig) expandEnv() {
	p.Type = strings.TrimSpace(os.ExpandEnv(p.Type))
	p.VaultAddress = strings.TrimSpace(os.ExpandEnv(p.VaultAddress))
	p.MainAddress = strings.TrimSpace(os.ExpandEnv(p.MainAddress))
	p.Credentials.TraderID = strings.TrimSpace(os.ExpandEnv(p.Credentials.TraderID))
	p.Credentials.SessionID = strings.TrimSpace(os.ExpandEnv(p.Credentials.SessionID))
	p.Credentials.Provider = strings.TrimSpace(os.ExpandEnv(p.Credentials.Provider))
	p.TimeoutRaw = strings.TrimSpace(os.ExpandEnv(p.TimeoutRaw))
}

func (p *ProviderConfig) parseDurations(name string) error {
	if p.TimeoutRaw == "" {
		p.Timeout = 0
		return nil
	}
	d, err := time.ParseDuration(p.TimeoutRaw)
	if err != nil {
		return fmt.Errorf("exchange provider %s: invalid timeout %q: %w", name, p.TimeoutRaw, err)
	}
	if d <= 0 {
		return fmt.Errorf("exchange provider %s: timeout must be positive, got %s", name, d)
	}
	p.Timeout = d
	return nil
}

// Validate ensures all providers have sane configuration.
func (c *Config) Validate() error {
	if len(c.Providers) == 0 {
		return fmt.Errorf("exchange config: providers cannot be empty")
	}
	if c.Default != "" {
		if _, ok := c.Providers[c.Default]; !ok {
			return fmt.Errorf("exchange config: default provider %q not defined", c.Default)
		}
	}

	for name, provider := range c.Providers {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("exchange config: provider name cannot be empty")
		}
		if err := provider.validate(name); err != nil {
			return err
		}
	}
	return nil
}

func (p *ProviderConfig) validate(name string) error {
	if p == nil {
		return fmt.Errorf("exchange config: provider %s is nil", name)
	}
	if strings.TrimSpace(p.Type) == "" {
		return fmt.Errorf("exchange config: provider %s must specify type", name)
	}

	if _, ok := lookupProviderBuilder(p.Type); !ok {
		return fmt.Errorf("exchange config: provider %s has unsupported type %q", name, p.Type)
	}

	return nil
}

// BuildProviders instantiates exchange providers according to the configuration.
func (c *Config) BuildProviders() (map[string]Provider, error) {
	return c.BuildProvidersWithSecrets(context.Background(), nil)
}

// BuildProvidersWithSecrets instantiates exchange providers with credentials resolved
// at construction time instead of storing expanded secrets in Config.
func (c *Config) BuildProvidersWithSecrets(ctx context.Context, store secrets.SecretStore) (map[string]Provider, error) {
	if store == nil {
		store = c.secretStore
	}
	if store == nil {
		store = secrets.NewEnvStore()
	}
	result := make(map[string]Provider, len(c.Providers))
	for name, providerCfg := range c.Providers {
		cfgCopy, err := providerCfg.withResolvedCredentials(ctx, name, store)
		if err != nil {
			return nil, err
		}
		builder, ok := lookupProviderBuilder(cfgCopy.Type)
		if !ok {
			return nil, fmt.Errorf("exchange provider %s: unsupported type %q", name, cfgCopy.Type)
		}
		provider, err := builder(name, cfgCopy)
		if err != nil {
			return nil, fmt.Errorf("exchange provider %s: %w", name, secrets.RedactError(err, cfgCopy.PrivateKey, cfgCopy.APIKey, cfgCopy.APISecret, cfgCopy.Passphrase))
		}
		result[name] = provider
	}
	return result, nil
}

func (p *ProviderConfig) withResolvedCredentials(ctx context.Context, providerName string, store secrets.SecretStore) (*ProviderConfig, error) {
	cfgCopy := *p
	if strings.ToLower(cfgCopy.Type) != "hyperliquid" {
		return &cfgCopy, nil
	}
	privateKey, err := resolveCredential(ctx, store, cfgCopy.PrivateKey, cfgCopy.secretKey("private_key"))
	if err != nil {
		return nil, fmt.Errorf("exchange provider %s: %w", providerName, err)
	}
	cfgCopy.PrivateKey = privateKey
	if cfgCopy.PrivateKey == "" {
		return nil, fmt.Errorf("exchange provider %s: requires private_key secret for %s", providerName, cfgCopy.secretKey("private_key").String())
	}
	return &cfgCopy, nil
}

func (p *ProviderConfig) secretKey(name string) secrets.Key {
	provider := p.Credentials.Provider
	if provider == "" {
		provider = p.Type
	}
	return secrets.Key{
		TraderID:  p.Credentials.TraderID,
		SessionID: p.Credentials.SessionID,
		Provider:  provider,
		Name:      name,
	}
}

func resolveCredential(ctx context.Context, store secrets.SecretStore, configured string, key secrets.Key) (string, error) {
	configured = strings.TrimSpace(configured)
	if configured != "" && !secrets.IsEnvReference(configured) {
		return configured, nil
	}
	if secret, ok := secrets.LookupEnvReference(configured); ok {
		if secret.Value != "" {
			return secret.Value, nil
		}
	}
	secret, err := store.Lookup(ctx, key)
	if err != nil {
		if configured != "" {
			return "", fmt.Errorf("resolve %s from env reference or secret store: %w", key.String(), err)
		}
		return "", fmt.Errorf("resolve %s: %w", key.String(), err)
	}
	return secret.Value, nil
}
