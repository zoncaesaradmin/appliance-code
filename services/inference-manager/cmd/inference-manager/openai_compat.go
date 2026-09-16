package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
)

const maxOpenAIProxyBody = 32 << 20

// openaiCompatConfig controls request rewrites for the active engine mode.
type openaiCompatConfig struct {
	// DisableStructuredOutputs avoids vLLM xgrammar / apply_grammar_bitmask on
	// CPU builds. Those paths call pin_memory and fatally kill EngineCore:
	// "pin_memory=True requires a CUDA or other accelerator backend".
	DisableStructuredOutputs bool
}

// normalizeOpenAIUpstreamBody adapts OpenAI-compatible request bodies to the
// subset the packaged inference runtime can serve without hanging or crashing.
//
// This is not client-specific: any agent using the OpenAI Responses API may
// send text.format.type=json_schema (Codex, Continue, custom SDKs, etc.).
// OpenAI documents text | json_object | json_schema. Packaged vLLM 0.17.x
// accepts json_schema on the request but crashes while streaming
// response.created (schema field alias dump bug). Remap that shape onto the
// runtime-supported constrained-generation path and keep streaming healthy.
// On CPU, never enable structured_outputs — use instruction guidance only.
func normalizeOpenAIUpstreamBody(path string, body []byte, opts openaiCompatConfig) ([]byte, bool) {
	path = strings.TrimSpace(path)
	switch {
	case path == "/v1/responses" || strings.HasPrefix(path, "/v1/responses?"):
		return normalizeResponsesBody(body, opts)
	default:
		return body, false
	}
}

func normalizeResponsesBody(body []byte, opts openaiCompatConfig) ([]byte, bool) {
	if len(bytes.TrimSpace(body)) == 0 {
		return body, false
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return body, false
	}
	changed := false
	if opts.DisableStructuredOutputs {
		if _, ok := payload["structured_outputs"]; ok {
			delete(payload, "structured_outputs")
			changed = true
		}
	}
	if normalizeResponsesTextFormat(payload, opts) {
		changed = true
	}
	if !changed {
		return body, false
	}
	out, err := json.Marshal(payload)
	if err != nil {
		return body, false
	}
	return out, true
}

func normalizeResponsesTextFormat(payload map[string]any, opts openaiCompatConfig) bool {
	text, _ := payload["text"].(map[string]any)
	if text == nil {
		return false
	}
	format, _ := text["format"].(map[string]any)
	if format == nil {
		return false
	}
	typeName, _ := format["type"].(string)
	if !strings.EqualFold(strings.TrimSpace(typeName), "json_schema") {
		return false
	}

	schema := extractJSONSchema(format)
	name, _ := format["name"].(string)
	name = strings.TrimSpace(name)
	if name == "" {
		if nested, ok := format["json_schema"].(map[string]any); ok {
			name, _ = nested["name"].(string)
			name = strings.TrimSpace(name)
		}
	}

	// OpenAI clients may send text.format.type=json_schema. Packaged vLLM
	// accepts that on the request, then crashes while streaming
	// response.created because ResponseFormatTextJSONSchemaConfig dumps the
	// Python field name schema_ instead of the OpenAI wire alias schema.
	// Always strip that format from the upstream body.
	delete(payload, "text")

	_, hasStructured := payload["structured_outputs"]
	hasTools := responsesHasTools(payload)

	// vLLM rejects specifying both structured_outputs and text.format.
	// Prefer constrained JSON generation when a schema is present and the
	// request is not also doing tool calling. Coding agents on the Responses
	// wire API commonly send tools + json_schema together; a grammar would
	// block tool-call tokens, so keep schema guidance in instructions only.
	// CPU engines must never take the grammar path (pin_memory crash).
	if opts.DisableStructuredOutputs {
		appendResponsesInstruction(payload, jsonSchemaInstruction(name, schema))
		return true
	}
	if hasStructured {
		appendResponsesInstruction(payload, jsonSchemaInstruction(name, schema))
		return true
	}
	if schema != nil && !hasTools {
		payload["structured_outputs"] = map[string]any{"json": schema}
		appendResponsesInstruction(payload, jsonSchemaInstruction(name, schema))
		return true
	}
	if hasTools {
		appendResponsesInstruction(payload, jsonSchemaInstruction(name, schema))
		return true
	}
	payload["text"] = map[string]any{
		"format": map[string]any{"type": "json_object"},
	}
	if name != "" {
		appendResponsesInstruction(payload, fmt.Sprintf("Respond with a JSON object for schema %q.", name))
	} else {
		appendResponsesInstruction(payload, "Respond with a JSON object.")
	}
	return true
}

func responsesHasTools(payload map[string]any) bool {
	tools, ok := payload["tools"].([]any)
	return ok && len(tools) > 0
}

func extractJSONSchema(format map[string]any) any {
	if schema, ok := format["schema"]; ok && schema != nil {
		return schema
	}
	// Some clients nest the OpenAI chat-completions shape under json_schema.
	if nested, ok := format["json_schema"].(map[string]any); ok {
		if schema, ok := nested["schema"]; ok && schema != nil {
			return schema
		}
		return nested
	}
	return nil
}

func appendResponsesInstruction(payload map[string]any, extra string) {
	extra = strings.TrimSpace(extra)
	if extra == "" {
		return
	}
	if existing, ok := payload["instructions"].(string); ok && strings.TrimSpace(existing) != "" {
		payload["instructions"] = strings.TrimSpace(existing) + "\n\n" + extra
		return
	}
	payload["instructions"] = extra
}

func jsonSchemaInstruction(name string, schema any) string {
	encoded, err := json.Marshal(schema)
	if err != nil || len(encoded) == 0 {
		if name != "" {
			return fmt.Sprintf("Respond with a JSON object matching schema %q.", name)
		}
		return "Respond with a JSON object."
	}
	if name != "" {
		return fmt.Sprintf("Respond with a single JSON object that validates against schema %q:\n%s", name, string(encoded))
	}
	return "Respond with a single JSON object that validates against this JSON Schema:\n" + string(encoded)
}

func prepareOpenAIProxyRequest(r *http.Request, opts openaiCompatConfig) error {
	if r == nil || r.Body == nil {
		return nil
	}
	if r.Method != http.MethodPost {
		return nil
	}
	path := r.URL.Path
	if path != "/v1/responses" && !strings.HasPrefix(path, "/v1/responses?") {
		return nil
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxOpenAIProxyBody+1))
	_ = r.Body.Close()
	if err != nil {
		return err
	}
	if len(raw) > maxOpenAIProxyBody {
		return fmt.Errorf("request body exceeds %d bytes", maxOpenAIProxyBody)
	}
	rewritten, changed := normalizeOpenAIUpstreamBody(path, raw, opts)
	if !changed {
		rewritten = raw
	}
	r.Body = io.NopCloser(bytes.NewReader(rewritten))
	r.ContentLength = int64(len(rewritten))
	r.Header.Set("Content-Length", strconv.Itoa(len(rewritten)))
	// Body was consumed; disable chunked leftovers.
	r.Header.Del("Transfer-Encoding")
	return nil
}

// openaiCompatOptions returns rewrite policy for the active device.
// CPU paths stay fail-closed against structured_outputs (grammar pin_memory).
func openaiCompatOptions(usingGPU bool) openaiCompatConfig {
	return openaiCompatConfig{
		DisableStructuredOutputs: !usingGPU,
	}
}

func newOpenAIReverseProxy(backend *url.URL) *httputil.ReverseProxy {
	proxy := httputil.NewSingleHostReverseProxy(backend)
	// Flush SSE / streamed OpenAI events as soon as chunks arrive.
	proxy.FlushInterval = -1
	defaultDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		defaultDirector(req)
		// Prefer the backend host for the engine; keep original path/query.
		if backend != nil {
			req.Host = backend.Host
		}
	}
	return proxy
}
