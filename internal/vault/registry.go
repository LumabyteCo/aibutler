package vault

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// ServiceEntry describes how a service authenticates.
type ServiceEntry struct {
	Name          string   `yaml:"name"`
	Domains       []string `yaml:"domains"`
	AuthType      string   `yaml:"auth_type"` // api_key, oauth2, bot_token, app_password, platform_token
	CredentialKey string   `yaml:"credential_key"`
	Header        string   `yaml:"header"` // e.g., "Authorization: Bearer {key}"
	OAuth         *OAuthConfig `yaml:"oauth,omitempty"`
}

// OAuthConfig holds OAuth-specific configuration.
type OAuthConfig struct {
	AuthorizeURL string   `yaml:"authorize_url"`
	TokenURL     string   `yaml:"token_url"`
	Scopes       []string `yaml:"scopes"`
	ScopesWrite  []string `yaml:"scopes_write,omitempty"`
}

// ServiceRegistry maps domains to credential keys and auth patterns.
type ServiceRegistry struct {
	services map[string]ServiceEntry // keyed by service name
	domains  map[string]string       // domain -> service name
}

// NewServiceRegistry loads the registry from a user-provided file.
// Falls back to embedded defaults if the file doesn't exist.
func NewServiceRegistry(userFile string) (*ServiceRegistry, error) {
	r := &ServiceRegistry{
		services: make(map[string]ServiceEntry),
		domains:  make(map[string]string),
	}

	// Load embedded defaults.
	r.addDefaults()

	// Override with user file if it exists.
	if userFile != "" {
		data, err := os.ReadFile(userFile)
		if err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("registry: read %s: %w", userFile, err)
		}
		if err == nil {
			var userServices map[string]ServiceEntry
			if err := yaml.Unmarshal(data, &userServices); err != nil {
				return nil, fmt.Errorf("registry: parse %s: %w", userFile, err)
			}
			for name, svc := range userServices {
				svc.Name = name
				r.services[name] = svc
				for _, d := range svc.Domains {
					r.domains[d] = name
				}
			}
		}
	}

	return r, nil
}

// Resolve finds the service entry for a given domain.
func (r *ServiceRegistry) Resolve(domain string) (ServiceEntry, bool) {
	name, ok := r.domains[domain]
	if !ok {
		return ServiceEntry{}, false
	}
	svc, ok := r.services[name]
	return svc, ok
}

// Services returns all registered services.
func (r *ServiceRegistry) Services() map[string]ServiceEntry {
	return r.services
}

// FindByCredentialKey finds a service entry by its credential key.
func (r *ServiceRegistry) FindByCredentialKey(key string) *ServiceEntry {
	for _, svc := range r.services {
		if svc.CredentialKey == key {
			return &svc
		}
	}
	return nil
}

func (r *ServiceRegistry) addDefaults() {
	defaults := []ServiceEntry{
		{Name: "openai", Domains: []string{"api.openai.com"}, AuthType: "api_key", CredentialKey: "openai", Header: "Authorization: Bearer {key}"},
		{Name: "anthropic", Domains: []string{"api.anthropic.com"}, AuthType: "api_key", CredentialKey: "anthropic", Header: "x-api-key: {key}"},
		{Name: "github", Domains: []string{"api.github.com"}, AuthType: "oauth2", CredentialKey: "github", Header: "Authorization: Bearer {key}"},
		{Name: "gmail", Domains: []string{"gmail.googleapis.com"}, AuthType: "oauth2", CredentialKey: "gmail", Header: "Authorization: Bearer {key}"},
		{Name: "google_calendar", Domains: []string{"www.googleapis.com"}, AuthType: "oauth2", CredentialKey: "google_calendar", Header: "Authorization: Bearer {key}"},
		{Name: "telegram", Domains: []string{"api.telegram.org"}, AuthType: "bot_token", CredentialKey: "telegram", Header: ""},
		{Name: "slack", Domains: []string{"slack.com", "api.slack.com"}, AuthType: "bot_token", CredentialKey: "slack", Header: "Authorization: Bearer {key}"},
		{Name: "openweathermap", Domains: []string{"api.openweathermap.org"}, AuthType: "api_key", CredentialKey: "openweathermap", Header: ""},
		{Name: "google_maps", Domains: []string{"maps.googleapis.com"}, AuthType: "api_key", CredentialKey: "google_maps", Header: ""},
		{Name: "newsapi", Domains: []string{"newsapi.org"}, AuthType: "api_key", CredentialKey: "newsapi", Header: ""},
		{Name: "alpha_vantage", Domains: []string{"www.alphavantage.co"}, AuthType: "api_key", CredentialKey: "alpha_vantage", Header: ""},
		{Name: "stability_ai", Domains: []string{"api.stability.ai"}, AuthType: "api_key", CredentialKey: "stability_ai", Header: "Authorization: Bearer {key}"},
		{Name: "elevenlabs", Domains: []string{"api.elevenlabs.io"}, AuthType: "api_key", CredentialKey: "elevenlabs", Header: "xi-api-key: {key}"},
		{Name: "deepgram", Domains: []string{"api.deepgram.com"}, AuthType: "api_key", CredentialKey: "deepgram", Header: "Authorization: Token {key}"},
		{Name: "tavily", Domains: []string{"api.tavily.com"}, AuthType: "api_key", CredentialKey: "tavily", Header: ""},
		{Name: "perplexity", Domains: []string{"api.perplexity.ai"}, AuthType: "api_key", CredentialKey: "perplexity", Header: "Authorization: Bearer {key}"},
		{Name: "deepl", Domains: []string{"api-free.deepl.com", "api.deepl.com"}, AuthType: "api_key", CredentialKey: "deepl", Header: "Authorization: DeepL-Auth-Key {key}"},
		{Name: "discord", Domains: []string{"discord.com", "api.discord.com"}, AuthType: "bot_token", CredentialKey: "discord", Header: "Authorization: Bot {key}"},
	}

	for _, svc := range defaults {
		r.services[svc.Name] = svc
		for _, d := range svc.Domains {
			r.domains[d] = svc.Name
		}
	}
}
