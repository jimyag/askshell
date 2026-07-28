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

	response, err := p.client.Responses.New(requestCtx, params)
	if err != nil {
		return Result{}, fmt.Errorf("OpenAI Responses API: %w", err)
	}
	output := strings.TrimSpace(response.OutputText())
	if output == "" {
		return Result{}, fmt.Errorf("OpenAI Responses API returned no text")
	}
	return Result{
		Text:             output,
		ReasoningSummary: responseReasoningSummary(response.Output),
	}, nil
}

func (p *openAIChat) Generate(ctx context.Context, request Request) (Result, error) {
	requestCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	response, err := p.client.Chat.Completions.New(requestCtx, openai.ChatCompletionNewParams{
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
	if err != nil {
		return Result{}, fmt.Errorf("OpenAI Chat Completions API: %w", err)
	}
	if len(response.Choices) == 0 {
		return Result{}, fmt.Errorf("OpenAI Chat Completions API returned no choices")
	}
	output := strings.TrimSpace(response.Choices[0].Message.Content)
	if output == "" {
		return Result{}, fmt.Errorf("OpenAI Chat Completions API returned no text")
	}
	return Result{Text: output}, nil
}

func responseReasoningSummary(items []responses.ResponseOutputItemUnion) string {
	var summary strings.Builder
	for _, item := range items {
		if item.Type != "reasoning" {
			continue
		}
		reasoning := item.AsReasoning()
		for _, part := range reasoning.Summary {
			text := strings.TrimSpace(part.Text)
			if text == "" {
				continue
			}
			if summary.Len() > 0 {
				summary.WriteString("\n")
			}
			summary.WriteString(text)
		}
	}
	return summary.String()
}
