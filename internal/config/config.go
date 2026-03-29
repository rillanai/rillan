package config

import "log/slog"

const (
	ProviderOpenAI    = "openai"
	ProviderAnthropic = "anthropic"

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

type Config struct {
	Server     ServerConfig       `yaml:"server"`
	Provider   ProviderConfig     `yaml:"provider"`
	Index      IndexConfig        `yaml:"index"`
	Retrieval  RetrievalConfig    `yaml:"retrieval"`
	Runtime    RuntimeConfig      `yaml:"runtime"`
	LocalModel LocalModelConfig   `yaml:"local_model"`
	Agent      AgentRuntimeConfig `yaml:"agent"`
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

type ProjectConfig struct {
	Name           string               `yaml:"name"`
	Classification string               `yaml:"classification"`
	Sources        []ProjectSource      `yaml:"sources"`
	Routing        ProjectRoutingConfig `yaml:"routing"`
	SystemPrompt   string               `yaml:"system_prompt"`
	Instructions   []string             `yaml:"instructions"`
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

func DefaultConfig() Config {
	return Config{
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
	}
}

func DefaultProjectConfig() ProjectConfig {
	return ProjectConfig{
		Classification: ProjectClassificationOpenSource,
		Sources:        []ProjectSource{},
		Routing: ProjectRoutingConfig{
			Default:   RoutePreferenceAuto,
			TaskTypes: map[string]string{},
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
