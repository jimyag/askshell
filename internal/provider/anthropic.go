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
	return p.GenerateStream(ctx, request, nil)
}

func (p *anthropicProvider) GenerateStream(
	ctx context.Context,
	request Request,
	emit Emit,
) (Result, error) {
	requestCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	stream := p.client.Messages.NewStreaming(requestCtx, anthropic.MessageNewParams{
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
	defer func() {
		_ = stream.Close()
	}()
	var output strings.Builder
	for stream.Next() {
		event := stream.Current()
		if event.Type != "content_block_delta" ||
			event.Delta.Type != "text_delta" ||
			event.Delta.Text == "" {
			continue
		}
		output.WriteString(event.Delta.Text)
		if err := emitText(emit, event.Delta.Text); err != nil {
			return Result{}, err
		}
	}
	if err := stream.Err(); err != nil {
		return Result{}, fmt.Errorf("anthropic Messages API: %w", err)
	}
	text := strings.TrimSpace(output.String())
	if text == "" {
		return Result{}, fmt.Errorf("anthropic Messages API returned no text")
	}
	return Result{Text: text}, nil
}
