package provider

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/jimyag/askshell/internal/config"
)

type anthropicProvider struct {
	client  anthropic.Client
	model   string
	timeout time.Duration
}

func newAnthropic(cfg config.Config) *anthropicProvider {
	options := []option.RequestOption{option.WithAPIKey(cfg.APIKey)}
	if cfg.BaseURL != "" {
		options = append(options, option.WithBaseURL(strings.TrimRight(cfg.BaseURL, "/")))
	}
	return &anthropicProvider{
		client:  anthropic.NewClient(options...),
		model:   cfg.Model,
		timeout: cfg.Timeout,
	}
}

func (p *anthropicProvider) Generate(ctx context.Context, request Request) (Result, error) {
	requestCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	message, err := p.client.Messages.New(requestCtx, anthropic.MessageNewParams{
		Model:     p.model,
		MaxTokens: 4096,
		System: []anthropic.TextBlockParam{
			{Text: request.Instructions},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(request.Input)),
		},
		OutputConfig: anthropic.OutputConfigParam{
			Format: anthropic.JSONOutputFormatParam{
				Schema: request.Schema,
			},
		},
	})
	if err != nil {
		return Result{}, fmt.Errorf("anthropic Messages API: %w", err)
	}

	var output strings.Builder
	for _, block := range message.Content {
		text := block.AsText().Text
		if text != "" {
			output.WriteString(text)
		}
	}
	if strings.TrimSpace(output.String()) == "" {
		return Result{}, fmt.Errorf("anthropic Messages API returned no text")
	}
	return Result{Text: strings.TrimSpace(output.String())}, nil
}
