package config

import "testing"

func TestValidateRejectsImplicitAnthropic(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Provider.Type = ProviderAnthropic
	cfg.Provider.Anthropic.APIKey = "anthropic-key"
	cfg.Provider.OpenAI.APIKey = ""

	err := Validate(cfg)
	if err == nil {
		t.Fatal("Validate returned nil error for implicit anthropic")
	}
}

func TestValidateAcceptsExplicitAnthropicOptIn(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Provider.Type = ProviderAnthropic
	cfg.Provider.Anthropic.Enabled = true
	cfg.Provider.Anthropic.APIKey = "anthropic-key"
	cfg.Provider.OpenAI.APIKey = ""

	if err := Validate(cfg); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
}

func TestValidateRequiresOpenAIKeyForDefaultProvider(t *testing.T) {
	cfg := DefaultConfig()

	err := Validate(cfg)
	if err == nil {
		t.Fatal("Validate returned nil error without an OpenAI key")
	}
}

func TestValidateForIndexRequiresRootOnly(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Index.Root = "/tmp/project"

	if err := ValidateForMode(cfg, ValidationModeIndex); err != nil {
		t.Fatalf("ValidateForMode returned error: %v", err)
	}
}

func TestValidateForServeRequiresProviderKey(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Index.Root = "/tmp/project"

	if err := ValidateForMode(cfg, ValidationModeServe); err == nil {
		t.Fatal("expected serve validation to require a provider key")
	}
}

func TestValidateForStatusDoesNotRequireRoot(t *testing.T) {
	cfg := DefaultConfig()

	if err := ValidateForMode(cfg, ValidationModeStatus); err != nil {
		t.Fatalf("ValidateForMode returned error: %v", err)
	}
}

func TestValidateLocalModelRequiresBaseURLWhenEnabled(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Provider.OpenAI.APIKey = "test-key"
	cfg.LocalModel.Enabled = true
	cfg.LocalModel.BaseURL = ""

	if err := Validate(cfg); err == nil {
		t.Fatal("expected validation error for empty base_url when local_model enabled")
	}
}

func TestValidateLocalModelRequiresEmbedModelWhenEnabled(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Provider.OpenAI.APIKey = "test-key"
	cfg.LocalModel.Enabled = true
	cfg.LocalModel.EmbedModel = ""

	if err := Validate(cfg); err == nil {
		t.Fatal("expected validation error for empty embed_model when local_model enabled")
	}
}

func TestValidateQueryRewriteRequiresLocalModelEnabled(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Provider.OpenAI.APIKey = "test-key"
	cfg.LocalModel.Enabled = false
	cfg.LocalModel.QueryRewrite.Enabled = true

	if err := Validate(cfg); err == nil {
		t.Fatal("expected validation error for query_rewrite.enabled without local_model.enabled")
	}
}

func TestValidateQueryRewriteRequiresModel(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Provider.OpenAI.APIKey = "test-key"
	cfg.LocalModel.Enabled = true
	cfg.LocalModel.QueryRewrite.Enabled = true
	cfg.LocalModel.QueryRewrite.Model = ""

	if err := Validate(cfg); err == nil {
		t.Fatal("expected validation error for empty query_rewrite.model")
	}
}

func TestValidateAcceptsEnabledLocalModel(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Provider.OpenAI.APIKey = "test-key"
	cfg.LocalModel.Enabled = true

	if err := Validate(cfg); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestValidateRejectsInvalidRetrievalBounds(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Provider.OpenAI.APIKey = "test-key"
	cfg.Retrieval.TopK = 0

	if err := Validate(cfg); err == nil {
		t.Fatal("expected invalid retrieval.top_k to fail validation")
	}

	cfg = DefaultConfig()
	cfg.Provider.OpenAI.APIKey = "test-key"
	cfg.Retrieval.MaxContextChars = 0

	if err := Validate(cfg); err == nil {
		t.Fatal("expected invalid retrieval.max_context_chars to fail validation")
	}
}

func TestValidateProjectRejectsEmptyName(t *testing.T) {
	cfg := DefaultProjectConfig()
	cfg.Classification = ProjectClassificationInternal

	if err := ValidateProject(cfg); err == nil {
		t.Fatal("expected empty project name to fail validation")
	}
}

func TestValidateProjectRejectsUnknownClassification(t *testing.T) {
	cfg := DefaultProjectConfig()
	cfg.Name = "demo"
	cfg.Classification = "classified"

	if err := ValidateProject(cfg); err == nil {
		t.Fatal("expected invalid classification to fail validation")
	}
}

func TestValidateProjectRejectsInvalidTaskRoute(t *testing.T) {
	cfg := DefaultProjectConfig()
	cfg.Name = "demo"
	cfg.Routing.TaskTypes["review"] = "somewhere"

	if err := ValidateProject(cfg); err == nil {
		t.Fatal("expected invalid task route to fail validation")
	}
}

func TestValidateProjectAcceptsValidConfig(t *testing.T) {
	cfg := DefaultProjectConfig()
	cfg.Name = "demo"
	cfg.Classification = ProjectClassificationProprietary
	cfg.Sources = []ProjectSource{{Path: "/repo/src", Type: "go"}}
	cfg.Routing.Default = RoutePreferencePreferCloud
	cfg.Routing.TaskTypes["code_generation"] = RoutePreferencePreferLocal
	cfg.SystemPrompt = "Keep responses grounded in the repo."
	cfg.Instructions = []string{"Never include credentials.", "Prefer retrieval before generation."}

	if err := ValidateProject(cfg); err != nil {
		t.Fatalf("ValidateProject returned error: %v", err)
	}
}

func TestValidateSystemRejectsMissingEncryptedPayload(t *testing.T) {
	cfg := DefaultSystemConfig()

	if err := ValidateSystem(cfg); err == nil {
		t.Fatal("expected missing encrypted payload to fail validation")
	}
}

func TestValidateSystemRejectsUnknownEncryptionMethod(t *testing.T) {
	cfg := DefaultSystemConfig()
	cfg.EncryptedPayload = "ciphertext"
	cfg.Encryption.Method = "plaintext"

	if err := ValidateSystem(cfg); err == nil {
		t.Fatal("expected unknown encryption method to fail validation")
	}
}

func TestValidateSystemAcceptsValidConfig(t *testing.T) {
	cfg := DefaultSystemConfig()
	cfg.EncryptedPayload = "ciphertext"

	if err := ValidateSystem(cfg); err != nil {
		t.Fatalf("ValidateSystem returned error: %v", err)
	}
}
