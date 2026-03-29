package config

import "log/slog"

const (
	SchemaVersionV1 = 1
	SchemaVersionV2 = 2

	ProviderOpenAI           = "openai"
	ProviderOpenAICompatible = "openai_compatible"
	ProviderAnthropic        = "anthropic"
	ProviderKimi             = "kimi"
	ProviderLocal            = "local"

	AuthStrategyNone        = "none"
	AuthStrategyAPIKey      = "api_key"
	AuthStrategyBrowserOIDC = "browser_oidc"
	AuthStrategyDeviceOIDC  = "device_oidc"

	SystemConfigVersion           = "m06"
	SystemEncryptionKeyringAESGCM = "keyring_aes_gcm"
	DefaultSystemKeyringService   = "rillan/system-policy"
	DefaultSystemKeyringAccount   = "machine-default"

	ProjectClassificationOpenSource  = "open_source"
	ProjectClassificationInternal    = "internal"
	ProjectClassificationProprietary = "proprietary"
	ProjectClassificationTradeSecret = "trade_secret"

	RoutePreferenceAuto        = "auto"
	RoutePreferencePreferLocal = "prefer_local"
	RoutePreferencePreferCloud = "prefer_cloud"
	RoutePreferenceLocalOnly   = "local_only"
)

// Config is the runtime configuration loaded from the main Rillan config file.
//
// Schema v2 keeps named LLM and MCP registries alongside the legacy provider
// shape that the current runtime still consumes internally.
type Config struct {
	SchemaVersion int                `yaml:"schema_version,omitempty"`
	Server        ServerConfig       `yaml:"server"`
	Provider      ProviderConfig     `yaml:"provider"`
	Index         IndexConfig        `yaml:"index"`
	Retrieval     RetrievalConfig    `yaml:"retrieval"`
	Runtime       RuntimeConfig      `yaml:"runtime"`
	LocalModel    LocalModelConfig   `yaml:"local_model"`
	Agent         AgentRuntimeConfig `yaml:"agent"`
	Auth          AuthConfig         `yaml:"auth,omitempty"`
	LLMs          LLMRegistryConfig  `yaml:"llms,omitempty"`
	MCPs          MCPRegistryConfig  `yaml:"mcps,omitempty"`
}

// AuthConfig stores non-secret control-plane auth metadata.
type AuthConfig struct {
	Rillan ControlPlaneAuthConfig `yaml:"rillan,omitempty"`
}

// ControlPlaneAuthConfig describes how Rillan should authenticate to its own
// team or control-plane endpoint.
type ControlPlaneAuthConfig struct {
	Endpoint     string `yaml:"endpoint,omitempty"`
	AuthStrategy string `yaml:"auth_strategy,omitempty"`
	SessionRef   string `yaml:"session_ref,omitempty"`
}

// LLMRegistryConfig stores named LLM provider entries and the active default.
type LLMRegistryConfig struct {
	Default   string              `yaml:"default,omitempty"`
	Providers []LLMProviderConfig `yaml:"providers,omitempty"`
}

// LLMProviderConfig describes one named LLM provider entry in schema v2.
//
// Secrets are referenced indirectly through CredentialRef rather than stored in
// plaintext config.
type LLMProviderConfig struct {
	ID            string   `yaml:"id,omitempty"`
	Type          string   `yaml:"type,omitempty"`
	Endpoint      string   `yaml:"endpoint,omitempty"`
	AuthStrategy  string   `yaml:"auth_strategy,omitempty"`
	DefaultModel  string   `yaml:"default_model,omitempty"`
	Capabilities  []string `yaml:"capabilities,omitempty"`
	CredentialRef string   `yaml:"credential_ref,omitempty"`
}

// MCPRegistryConfig stores named MCP endpoint entries and the active default.
type MCPRegistryConfig struct {
	Default string            `yaml:"default,omitempty"`
	Servers []MCPServerConfig `yaml:"servers,omitempty"`
}

// MCPServerConfig describes one named MCP endpoint entry in schema v2.
type MCPServerConfig struct {
	ID           string `yaml:"id,omitempty"`
	Endpoint     string `yaml:"endpoint,omitempty"`
	Transport    string `yaml:"transport,omitempty"`
	AuthStrategy string `yaml:"auth_strategy,omitempty"`
	ReadOnly     bool   `yaml:"read_only,omitempty"`
	SessionRef   string `yaml:"session_ref,omitempty"`
}

type SystemConfig struct {
	Version          string                 `yaml:"version"`
	Encryption       SystemEncryptionConfig `yaml:"encryption"`
	EncryptedPayload string                 `yaml:"encrypted_payload"`
	Policy           SystemPolicy           `yaml:"-"`
}

type SystemEncryptionConfig struct {
	Method         string `yaml:"method"`
	KeyringService string `yaml:"keyring_service"`
	KeyringAccount string `yaml:"keyring_account"`
}

type SystemPolicy struct {
	Identity SystemIdentityRules
	Rules    SystemPolicyRules
}

type SystemIdentityRules struct {
	People             []string
	Employers          []string
	PIIPatterns        []string
	CredentialPatterns []string
}

type SystemPolicyRules struct {
	MaskPIIForRemote          bool
	StripEmployerReferences   bool
	ForceLocalForTradeSecret  bool
	BlockRemoteOnPCIArtifacts bool
}

// ProjectConfig is the repo-local `.sidekick/project.yaml` configuration.
type ProjectConfig struct {
	Name           string                         `yaml:"name"`
	Classification string                         `yaml:"classification"`
	Sources        []ProjectSource                `yaml:"sources"`
	Routing        ProjectRoutingConfig           `yaml:"routing"`
	Providers      ProjectProviderSelectionConfig `yaml:"providers,omitempty"`
	Agent          ProjectAgentConfig             `yaml:"agent,omitempty"`
	SystemPrompt   string                         `yaml:"system_prompt"`
	Instructions   []string                       `yaml:"instructions"`
}

// ProjectProviderSelectionConfig captures repo-local provider choices.
type ProjectProviderSelectionConfig struct {
	LLMDefault string   `yaml:"llm_default,omitempty"`
	LLMAllowed []string `yaml:"llm_allowed,omitempty"`
	MCPEnabled []string `yaml:"mcp_enabled,omitempty"`
}

// ProjectAgentConfig stores repo-local agent selections.
type ProjectAgentConfig struct {
	Skills ProjectSkillSelectionConfig `yaml:"skills,omitempty"`
}

// ProjectSkillSelectionConfig stores repo-local enabled markdown skill IDs.
type ProjectSkillSelectionConfig struct {
	Enabled []string `yaml:"enabled,omitempty"`
}

type ProjectSource struct {
	Path string `yaml:"path"`
	Type string `yaml:"type"`
}

type ProjectRoutingConfig struct {
	Default   string            `yaml:"default"`
	TaskTypes map[string]string `yaml:"task_types"`
}

type LocalModelConfig struct {
	Enabled      bool               `yaml:"enabled"`
	BaseURL      string             `yaml:"base_url"`
	EmbedModel   string             `yaml:"embed_model"`
	QueryRewrite QueryRewriteConfig `yaml:"query_rewrite"`
}

type QueryRewriteConfig struct {
	Enabled bool   `yaml:"enabled"`
	Model   string `yaml:"model"`
}

type ServerConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	LogLevel string `yaml:"log_level"`
}

type ProviderConfig struct {
	Type      string             `yaml:"type"`
	OpenAI    OpenAIConfig       `yaml:"openai"`
	Anthropic AnthropicConfig    `yaml:"anthropic"`
	Local     LocalModelProvider `yaml:"local"`
}

type OpenAIConfig struct {
	BaseURL string `yaml:"base_url"`
	APIKey  string `yaml:"api_key"`
}

type AnthropicConfig struct {
	Enabled bool   `yaml:"enabled"`
	BaseURL string `yaml:"base_url"`
	APIKey  string `yaml:"api_key"`
}

type LocalModelProvider struct {
	BaseURL string `yaml:"base_url"`
}

type IndexConfig struct {
	Root           string   `yaml:"root"`
	Includes       []string `yaml:"includes"`
	Excludes       []string `yaml:"excludes"`
	ChunkSizeLines int      `yaml:"chunk_size_lines"`
}

type RuntimeConfig struct {
	VectorStoreMode   string `yaml:"vector_store_mode"`
	LocalModelBaseURL string `yaml:"local_model_base_url"`
}

type RetrievalConfig struct {
	Enabled         bool `yaml:"enabled"`
	TopK            int  `yaml:"top_k"`
	MaxContextChars int  `yaml:"max_context_chars"`
}

type AgentRuntimeConfig struct {
	Enabled bool      `yaml:"enabled"`
	MCP     MCPConfig `yaml:"mcp"`
}

type MCPConfig struct {
	Enabled        bool `yaml:"enabled"`
	ReadOnly       bool `yaml:"read_only"`
	MaxOpenFiles   int  `yaml:"max_open_files"`
	MaxDiagnostics int  `yaml:"max_diagnostics"`
}

// DefaultConfig returns the default runtime configuration, including schema-v2
// registries and legacy runtime defaults.
func DefaultConfig() Config {
	return Config{
		SchemaVersion: SchemaVersionV2,
		Server: ServerConfig{
			Host:     "127.0.0.1",
			Port:     8420,
			LogLevel: "info",
		},
		Provider: ProviderConfig{
			Type: ProviderOpenAI,
			OpenAI: OpenAIConfig{
				BaseURL: "https://api.openai.com/v1",
			},
			Anthropic: AnthropicConfig{
				Enabled: false,
				BaseURL: "https://api.anthropic.com",
			},
			Local: LocalModelProvider{
				BaseURL: "http://127.0.0.1:11434",
			},
		},
		Index: IndexConfig{
			Excludes:       []string{".git", "node_modules", ".direnv", ".idea"},
			ChunkSizeLines: 120,
		},
		Retrieval: RetrievalConfig{
			Enabled:         false,
			TopK:            4,
			MaxContextChars: 6000,
		},
		Runtime: RuntimeConfig{
			VectorStoreMode:   "embedded",
			LocalModelBaseURL: "http://127.0.0.1:11434",
		},
		LocalModel: LocalModelConfig{
			Enabled:    false,
			BaseURL:    "http://127.0.0.1:11434",
			EmbedModel: "nomic-embed-text",
			QueryRewrite: QueryRewriteConfig{
				Enabled: false,
				Model:   "qwen3:0.6b",
			},
		},
		Agent: AgentRuntimeConfig{
			Enabled: false,
			MCP: MCPConfig{
				Enabled:        false,
				ReadOnly:       true,
				MaxOpenFiles:   8,
				MaxDiagnostics: 20,
			},
		},
		LLMs: LLMRegistryConfig{
			Providers: []LLMProviderConfig{},
		},
		MCPs: MCPRegistryConfig{
			Servers: []MCPServerConfig{},
		},
	}
}

// DefaultProjectConfig returns the default repo-local project configuration.
func DefaultProjectConfig() ProjectConfig {
	return ProjectConfig{
		Classification: ProjectClassificationOpenSource,
		Sources:        []ProjectSource{},
		Routing: ProjectRoutingConfig{
			Default:   RoutePreferenceAuto,
			TaskTypes: map[string]string{},
		},
		Providers: ProjectProviderSelectionConfig{
			LLMAllowed: []string{},
			MCPEnabled: []string{},
		},
		Agent: ProjectAgentConfig{
			Skills: ProjectSkillSelectionConfig{
				Enabled: []string{},
			},
		},
		Instructions: []string{},
	}
}

func DefaultSystemConfig() SystemConfig {
	return SystemConfig{
		Version: SystemConfigVersion,
		Encryption: SystemEncryptionConfig{
			Method:         SystemEncryptionKeyringAESGCM,
			KeyringService: DefaultSystemKeyringService,
			KeyringAccount: DefaultSystemKeyringAccount,
		},
	}
}

func ParseLogLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
