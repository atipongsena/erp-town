package agents

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	anthropicoption "github.com/anthropics/anthropic-sdk-go/option"
	openai "github.com/openai/openai-go/v3"
	openaioption "github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
)

// LocalProviderConfig deliberately has no API key or external enable switch.
// These adapters exercise the pinned native SDK contracts against loopback
// fixtures. Selecting a paid endpoint requires a separately approved change.
type LocalProviderConfig struct{ Model, BaseURL string }

var (
	errProviderProtocol = errors.New("agents: provider response unavailable or invalid")
	errProviderDisabled = errors.New("agents: external provider disabled")
)

func NewLocalProvider(kind string, cfg LocalProviderConfig) (Provider, error) {
	u, err := url.Parse(cfg.BaseURL)
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || !net.ParseIP(u.Hostname()).IsLoopback() || cfg.Model == "" {
		return nil, errProviderDisabled
	}
	// Disable proxy environment variables and redirects: even an approved local
	// base must not forward prompts/authentication to an external proxy or target.
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DisableKeepAlives = true
	client := &http.Client{Transport: localProviderTransport{baseHost: u.Host, transport: transport}, Timeout: 2 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	switch kind {
	case "anthropic":
		c := anthropic.NewClient(anthropicoption.WithAPIKey("local-fixture"), anthropicoption.WithAuthToken(""), anthropicoption.WithHeaderDel("Authorization"), anthropicoption.WithBaseURL(cfg.BaseURL), anthropicoption.WithHTTPClient(client), anthropicoption.WithMaxRetries(0))
		return &anthropicProvider{client: c, model: anthropic.Model(cfg.Model)}, nil
	case "openai":
		c := openai.NewClient(openaioption.WithAPIKey("local-fixture"), openaioption.WithBaseURL(cfg.BaseURL), openaioption.WithHTTPClient(client), openaioption.WithMaxRetries(0))
		return &openAIProvider{client: c, model: cfg.Model}, nil
	default:
		return nil, errProviderDisabled
	}
}

type localProviderTransport struct {
	baseHost  string
	transport http.RoundTripper
}

func (t localProviderTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Scheme != "http" || r.URL.Host != t.baseHost {
		return nil, errProviderDisabled
	}
	res, err := t.transport.RoundTrip(r)
	if err != nil {
		return nil, errProviderProtocol
	}
	res.Body = &boundedProviderBody{body: res.Body, remaining: 256 * 1024}
	return res, nil
}

type boundedProviderBody struct {
	body      io.ReadCloser
	remaining int
}

func (b *boundedProviderBody) Read(p []byte) (int, error) {
	if b.remaining == 0 {
		var extra [1]byte
		n, err := b.body.Read(extra[:])
		if n != 0 {
			return 0, errProviderProtocol
		}
		return 0, err
	}
	if len(p) > b.remaining {
		p = p[:b.remaining]
	}
	n, err := b.body.Read(p)
	b.remaining -= n
	return n, err
}
func (b *boundedProviderBody) Close() error { return b.body.Close() }

func validProviderRequest(req Request) bool {
	raw, err := json.Marshal(req)
	return err == nil && len(raw) <= 64*1024 && req.RemainingTokens > 0 && req.RemainingTokens <= 12000 && strings.TrimSpace(req.Instruction) != "" && len(req.History) < 8 && len(req.Tools) <= 16
}

type anthropicProvider struct {
	client anthropic.Client
	model  anthropic.Model
}

func (p *anthropicProvider) Step(ctx context.Context, req Request) (Response, error) {
	if !validProviderRequest(req) {
		return Response{}, errProviderProtocol
	}
	messages := []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(req.Instruction))}
	for _, turn := range req.History {
		content := []anthropic.ContentBlockParamUnion{}
		if turn.Text != "" {
			content = append(content, anthropic.NewTextBlock(turn.Text))
		}
		for _, c := range turn.Calls {
			content = append(content, anthropic.NewToolUseBlock(c.ID, json.RawMessage(c.Arguments), c.Tool))
		}
		messages = append(messages, anthropic.NewAssistantMessage(content...))
		results := []anthropic.ContentBlockParamUnion{}
		for _, r := range turn.Results {
			results = append(results, anthropic.NewToolResultBlock(r.ID, string(r.Output), false))
		}
		messages = append(messages, anthropic.NewUserMessage(results...))
	}
	tools := []anthropic.ToolUnionParam{}
	for _, tool := range req.Tools {
		var schema anthropic.ToolInputSchemaParam
		if json.Unmarshal(tool.InputSchema, &schema) != nil {
			return Response{}, errProviderProtocol
		}
		tools = append(tools, anthropic.ToolUnionParam{OfTool: &anthropic.ToolParam{Name: tool.Name, Description: anthropic.String(tool.Description), InputSchema: schema}})
	}
	msg, err := p.client.Messages.New(ctx, anthropic.MessageNewParams{Model: p.model, MaxTokens: req.RemainingTokens, Messages: messages, Tools: tools})
	if err != nil || !msg.Usage.JSON.InputTokens.Valid() || !msg.Usage.JSON.OutputTokens.Valid() {
		return Response{}, errProviderProtocol
	}
	out := Response{Usage: Usage{InputTokens: msg.Usage.InputTokens, OutputTokens: msg.Usage.OutputTokens}}
	for _, block := range msg.Content {
		switch block.Type {
		case "text":
			out.Text += block.Text
		case "tool_use":
			out.Calls = append(out.Calls, Call{ID: block.ID, Tool: block.Name, Arguments: append(json.RawMessage(nil), block.Input...)})
		default:
			return Response{}, errProviderProtocol
		}
	}
	if !validProviderResponse(out) {
		return Response{}, errProviderProtocol
	}
	return out, nil
}

type openAIProvider struct {
	client openai.Client
	model  string
}

func (p *openAIProvider) Step(ctx context.Context, req Request) (Response, error) {
	if !validProviderRequest(req) {
		return Response{}, errProviderProtocol
	}
	input := []responses.ResponseInputItemUnionParam{responses.ResponseInputItemParamOfMessage(req.Instruction, responses.EasyInputMessageRoleUser)}
	for _, turn := range req.History {
		if turn.Text != "" {
			input = append(input, responses.ResponseInputItemParamOfMessage(turn.Text, responses.EasyInputMessageRoleAssistant))
		}
		for _, c := range turn.Calls {
			input = append(input, responses.ResponseInputItemParamOfFunctionCall(string(c.Arguments), c.ID, c.Tool))
		}
		for _, r := range turn.Results {
			item := responses.ResponseInputItemParamOfFunctionCallOutput(string(r.Output))
			item.OfFunctionCallOutput.CallID = openai.String(r.ID)
			input = append(input, item)
		}
	}
	tools := []responses.ToolUnionParam{}
	for _, tool := range req.Tools {
		var schema map[string]any
		if json.Unmarshal(tool.InputSchema, &schema) != nil {
			return Response{}, errProviderProtocol
		}
		tools = append(tools, responses.ToolUnionParam{OfFunction: &responses.FunctionToolParam{Name: tool.Name, Description: openai.String(tool.Description), Parameters: schema, Strict: openai.Bool(false)}})
	}
	msg, err := p.client.Responses.New(ctx, responses.ResponseNewParams{Model: openai.ChatModel(p.model), Input: responses.ResponseNewParamsInputUnion{OfInputItemList: input}, Tools: tools, MaxOutputTokens: openai.Int(req.RemainingTokens), Store: openai.Bool(false), ParallelToolCalls: openai.Bool(false)})
	if err != nil || msg.Status != "completed" || !msg.Usage.JSON.InputTokens.Valid() || !msg.Usage.JSON.OutputTokens.Valid() {
		return Response{}, errProviderProtocol
	}
	out := Response{Usage: Usage{InputTokens: msg.Usage.InputTokens, OutputTokens: msg.Usage.OutputTokens}}
	for _, item := range msg.Output {
		switch item.Type {
		case "function_call":
			c := item.AsFunctionCall()
			out.Calls = append(out.Calls, Call{ID: c.CallID, Tool: c.Name, Arguments: json.RawMessage(c.Arguments)})
		case "message":
			m := item.AsMessage()
			for _, c := range m.Content {
				if c.Type != "output_text" {
					return Response{}, errProviderProtocol
				}
				out.Text += c.Text
			}
		case "reasoning": // No reasoning contents or remote state are retained.
		default:
			return Response{}, errProviderProtocol
		}
	}
	if !validProviderResponse(out) {
		return Response{}, errProviderProtocol
	}
	return out, nil
}

func validProviderResponse(out Response) bool {
	raw, err := json.Marshal(out)
	if err != nil || len(raw) > 64*1024 || out.Usage.InputTokens < 0 || out.Usage.OutputTokens < 0 || len(out.Calls) > 16 || (len(out.Calls) == 0 && strings.TrimSpace(out.Text) == "") {
		return false
	}
	for _, c := range out.Calls {
		if !validID(c.ID) || !validID(c.Tool) || !jsonObject(c.Arguments) {
			return false
		}
	}
	return true
}
