package provider

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jimyag/askshell/internal/config"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"
)

type openAIResponses struct {
	client  openai.Client
	model   string
	timeout time.Duration
}

type openAIChat struct {
	client  openai.Client
	model   string
	timeout time.Duration
}

func newOpenAIResponses(cfg config.Config) *openAIResponses {
	return &openAIResponses{
		client:  newOpenAIClient(cfg),
		model:   cfg.Model,
		timeout: cfg.Timeout,
	}
}

func newOpenAIChat(cfg config.Config) *openAIChat {
	return &openAIChat{
		client:  newOpenAIClient(cfg),
		model:   cfg.Model,
		timeout: cfg.Timeout,
	}
}

func newOpenAIClient(cfg config.Config) openai.Client {
	options := []option.RequestOption{option.WithAPIKey(cfg.APIKey)}
	if cfg.BaseURL != "" {
		options = append(options, option.WithBaseURL(strings.TrimRight(cfg.BaseURL, "/")))
	}
	return openai.NewClient(options...)
}

func (p *openAIResponses) Generate(ctx context.Context, request Request) (Result, error) {
	return p.GenerateStream(ctx, request, nil)
}

func (p *openAIResponses) GenerateStream(
	ctx context.Context,
	request Request,
	emit Emit,
) (Result, error) {
	requestCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	params := responses.ResponseNewParams{
		Model:        p.model,
		Instructions: openai.String(request.Instructions),
		ContextManagement: []responses.ResponseNewParamsContextManagement{
			{
				Type:             "compaction",
				CompactThreshold: openai.Int(12000),
			},
		},
		Input: responses.ResponseNewParamsInputUnion{
			OfString: openai.String(request.Input),
		},
		Store: openai.Bool(false),
		Text: responses.ResponseTextConfigParam{
			Format: responses.ResponseFormatTextConfigParamOfJSONSchema(
				"askshell_answer",
				request.Schema,
			),
		},
	}
	if request.ReasoningSummary {
		params.Reasoning = shared.ReasoningParam{
			Summary: shared.ReasoningSummaryConcise,
		}
	}

	stream := p.client.Responses.NewStreaming(requestCtx, params)
	defer func() {
		_ = stream.Close()
	}()
	var result Result
	for stream.Next() {
		switch event := stream.Current(); event.Type {
		case "response.output_text.delta":
			result.Text += event.Delta
			if err := emitText(emit, event.Delta); err != nil {
				return Result{}, err
			}
		case "response.reasoning_summary_text.delta":
			result.ReasoningSummary += event.Delta
		}
	}
	if err := stream.Err(); err != nil {
		return Result{}, fmt.Errorf("OpenAI Responses API: %w", err)
	}
	result.Text = strings.TrimSpace(result.Text)
	if result.Text == "" {
		return Result{}, fmt.Errorf("OpenAI Responses API returned no text")
	}
	result.ReasoningSummary = strings.TrimSpace(result.ReasoningSummary)
	return result, nil
}

func (p *openAIChat) Generate(ctx context.Context, request Request) (Result, error) {
	return p.GenerateStream(ctx, request, nil)
}

func (p *openAIChat) GenerateStream(
	ctx context.Context,
	request Request,
	emit Emit,
) (Result, error) {
	requestCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	stream := p.client.Chat.Completions.NewStreaming(requestCtx, openai.ChatCompletionNewParams{
		Model: p.model,
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(request.Instructions),
			openai.UserMessage(request.Input),
		},
		ResponseFormat: openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONSchema: &openai.ResponseFormatJSONSchemaParam{
				JSONSchema: openai.ResponseFormatJSONSchemaJSONSchemaParam{
					Name:   "askshell_answer",
					Strict: openai.Bool(true),
					Schema: request.Schema,
				},
			},
		},
	})
	defer func() {
		_ = stream.Close()
	}()
	var output strings.Builder
	for stream.Next() {
		delta := stream.Current().Choices
		if len(delta) == 0 || delta[0].Delta.Content == "" {
			continue
		}
		chunk := delta[0].Delta.Content
		output.WriteString(chunk)
		if err := emitText(emit, chunk); err != nil {
			return Result{}, err
		}
	}
	if err := stream.Err(); err != nil {
		return Result{}, fmt.Errorf("OpenAI Chat Completions API: %w", err)
	}
	text := strings.TrimSpace(output.String())
	if text == "" {
		return Result{}, fmt.Errorf("OpenAI Chat Completions API returned no text")
	}
	return Result{Text: text}, nil
}

func emitText(emit Emit, text string) error {
	if emit == nil {
		return nil
	}
	return emit(text)
}
