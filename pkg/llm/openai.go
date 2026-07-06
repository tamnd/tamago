package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// openaiClient talks to any OpenAI-compatible chat completions endpoint with
// plain HTTP and SSE streaming. Such servers rarely honor json_schema output,
// so JSON requests embed the schema in the prompt and parse tolerantly.
type openaiClient struct {
	baseURL  string
	apiKey   string
	designer string
	tiers    map[string]string
	hc       *http.Client
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func newOpenAI() *openaiClient {
	return &openaiClient{
		baseURL:  strings.TrimRight(envOr("OPENAI_BASE_URL", "https://api.openai.com/v1"), "/"),
		apiKey:   os.Getenv("OPENAI_API_KEY"),
		designer: envOr("TAMAGO_OPENAI_MODEL", "gpt-5"),
		tiers: map[string]string{
			"fast":     envOr("TAMAGO_MODEL_FAST", "gpt-5-mini"),
			"standard": envOr("TAMAGO_MODEL_STANDARD", "gpt-5"),
			"deep":     envOr("TAMAGO_MODEL_DEEP", "gpt-5"),
		},
		hc: &http.Client{}, // no total timeout, streams run long; ctx cancels
	}
}

func (cl *openaiClient) Name() string          { return "openai" }
func (cl *openaiClient) DesignerModel() string { return cl.designer }

func (cl *openaiClient) ModelForTier(tier string) string {
	if m, ok := cl.tiers[tier]; ok {
		return m
	}
	return cl.tiers["standard"]
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// transientStatus reports whether a retry could plausibly succeed.
func transientStatus(code int) bool {
	switch code {
	case http.StatusTooManyRequests, http.StatusInternalServerError,
		http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return false
}

// StreamText retries transient failures (429, 5xx) with a short backoff.
// A retry is only issued before any delta has streamed, so callers never
// see duplicated output.
func (cl *openaiClient) StreamText(ctx context.Context, model, system, user string, maxTokens int64, onDelta func(string)) (string, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(time.Duration(attempt*2) * time.Second):
			}
		}
		out, retry, err := cl.streamOnce(ctx, model, system, user, maxTokens, onDelta)
		if err == nil {
			return out, nil
		}
		lastErr = err
		if !retry {
			return "", err
		}
	}
	return "", lastErr
}

func (cl *openaiClient) streamOnce(ctx context.Context, model, system, user string, maxTokens int64, onDelta func(string)) (string, bool, error) {
	msgs := []chatMessage{}
	if system != "" {
		msgs = append(msgs, chatMessage{Role: "system", Content: system})
	}
	msgs = append(msgs, chatMessage{Role: "user", Content: user})
	body := map[string]any{
		"model":      model,
		"messages":   msgs,
		"stream":     true,
		"max_tokens": maxTokens,
	}
	b, err := json.Marshal(body)
	if err != nil {
		return "", false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cl.baseURL+"/chat/completions", bytes.NewReader(b))
	if err != nil {
		return "", false, err
	}
	req.Header.Set("Content-Type", "application/json")
	if cl.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+cl.apiKey)
	}
	resp, err := cl.hc.Do(req)
	if err != nil {
		return "", ctx.Err() == nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		if resp.StatusCode == http.StatusUnauthorized {
			return "", false, fmt.Errorf("authentication failed against %s: set OPENAI_API_KEY: %s", cl.baseURL, msg)
		}
		return "", transientStatus(resp.StatusCode), fmt.Errorf("%s returned %d: %s", cl.baseURL, resp.StatusCode, msg)
	}

	var sb strings.Builder
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue // keep-alives and vendor extras are not fatal
		}
		for _, c := range chunk.Choices {
			if c.Delta.Content != "" {
				sb.WriteString(c.Delta.Content)
				if onDelta != nil {
					onDelta(c.Delta.Content)
				}
			}
		}
	}
	if err := sc.Err(); err != nil {
		return "", false, err
	}
	return sb.String(), false, nil
}

func (cl *openaiClient) JSON(ctx context.Context, model, system, user string, maxTokens int64, schema map[string]any, out any, onDelta func(string)) error {
	sb, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		return err
	}
	prompt := fmt.Sprintf("%s\n\nAnswer with ONE JSON object matching this JSON Schema. No prose before or after it, no code fences.\n\nSchema:\n%s", user, sb)
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			prompt += fmt.Sprintf("\n\nYour previous answer was not valid JSON (%v). Reply with only the JSON object.", lastErr)
		}
		text, err := cl.StreamText(ctx, model, system, prompt, maxTokens, onDelta)
		if err != nil {
			return err
		}
		raw, err := ExtractJSON(text)
		if err != nil {
			lastErr = err
			continue
		}
		if err := json.Unmarshal([]byte(raw), out); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	return fmt.Errorf("model returned invalid JSON: %w", lastErr)
}

// ExtractJSON pulls the first complete JSON object out of a possibly chatty
// reply: code fences, prose before or after, all tolerated.
func ExtractJSON(s string) (string, error) {
	start := strings.IndexByte(s, '{')
	if start < 0 {
		return "", fmt.Errorf("no JSON object in the reply")
	}
	depth := 0
	inStr := false
	escaped := false
	for i := start; i < len(s); i++ {
		c := s[i]
		if inStr {
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start : i+1], nil
			}
		}
	}
	return "", fmt.Errorf("unbalanced JSON object in the reply")
}
