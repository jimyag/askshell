package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

type Provider string

const (
	ProviderOpenAIResponses Provider = "openai-responses"
	ProviderOpenAIChat      Provider = "openai-chat"
	ProviderAnthropic       Provider = "anthropic"
)

type Config struct {
	Provider Provider
	BaseURL  string
	APIKey   string
	Model    string
	Timeout  time.Duration
}

func Load() (Config, error) {
	provider := Provider(strings.TrimSpace(os.Getenv("ASKSHELL_PROVIDER")))
	if provider == "" {
		switch {
		case os.Getenv("OPENAI_API_KEY") != "":
			provider = ProviderOpenAIResponses
		case os.Getenv("ANTHROPIC_API_KEY") != "":
			provider = ProviderAnthropic
		default:
			provider = ProviderOpenAIChat
		}
	}
	if provider != ProviderOpenAIResponses &&
		provider != ProviderOpenAIChat &&
		provider != ProviderAnthropic {
		return Config{}, fmt.Errorf(
			"invalid ASKSHELL_PROVIDER %q; use openai-responses, openai-chat, or anthropic",
			provider,
		)
	}

	cfg := Config{
		Provider: provider,
		Timeout:  durationOr("ASKSHELL_TIMEOUT", 2*time.Minute),
	}
	switch provider {
	case ProviderAnthropic:
		cfg.APIKey = firstNonEmpty(os.Getenv("ASKSHELL_API_KEY"), os.Getenv("ANTHROPIC_API_KEY"))
		cfg.BaseURL = firstNonEmpty(os.Getenv("ASKSHELL_BASE_URL"), os.Getenv("ANTHROPIC_BASE_URL"))
		cfg.Model = modelOr("claude-haiku-4-5")
		if cfg.APIKey == "" {
			return Config{}, fmt.Errorf("ANTHROPIC_API_KEY or ASKSHELL_API_KEY is required")
		}
	case ProviderOpenAIResponses:
		cfg.APIKey = firstNonEmpty(
			os.Getenv("ASKSHELL_API_KEY"),
			os.Getenv("OPENAI_API_KEY"),
		)
		cfg.BaseURL = firstNonEmpty(os.Getenv("ASKSHELL_BASE_URL"), os.Getenv("OPENAI_BASE_URL"))
		cfg.Model = modelOr("gpt-5.6-luna")
		if cfg.APIKey == "" {
			return Config{}, fmt.Errorf(
				"OPENAI_API_KEY or ASKSHELL_API_KEY is required",
			)
		}
	case ProviderOpenAIChat:
		cfg.APIKey = firstNonEmpty(os.Getenv("ASKSHELL_API_KEY"), os.Getenv("OPENAI_API_KEY"))
		cfg.BaseURL = firstNonEmpty(os.Getenv("ASKSHELL_BASE_URL"), os.Getenv("OPENAI_BASE_URL"))
		if cfg.APIKey == "" && cfg.BaseURL == "" {
			cfg.APIKey = "ollama"
			cfg.BaseURL = "http://127.0.0.1:11434/v1"
			cfg.Model = modelOr("qwen3:4b")
		} else {
			cfg.Model = modelOr("gpt-5.6-luna")
		}
	}
	return cfg, nil
}

func modelOr(fallback string) string {
	value := strings.TrimSpace(os.Getenv("ASKSHELL_MODEL"))
	if value == "" {
		return fallback
	}
	return value
}

func durationOr(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
