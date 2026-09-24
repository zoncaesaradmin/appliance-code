package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestNormalizeResponsesJSONSchemaUsesStructuredOutputs(t *testing.T) {
	in := []byte(`{
		"model": "Qwen/Qwen2.5-3B-Instruct",
		"input": "hello",
		"stream": true,
		"instructions": "Be helpful.",
		"text": {
			"format": {
				"type": "json_schema",
				"name": "agent_plan",
				"strict": true,
				"schema": {
					"type": "object",
					"properties": {"ok": {"type": "boolean"}},
					"required": ["ok"],
					"additionalProperties": false
				}
			}
		}
	}`)
	out, changed := normalizeOpenAIUpstreamBody("/v1/responses", in, openaiCompatConfig{})
	if !changed {
		t.Fatal("expected normalization")
	}
	var payload map[string]any
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatal(err)
	}
	if _, ok := payload["text"]; ok {
		t.Fatalf("text.format json_schema must be removed to avoid vLLM stream crash: %#v", payload["text"])
	}
	structured, ok := payload["structured_outputs"].(map[string]any)
	if !ok {
		t.Fatalf("structured_outputs missing: %#v", payload)
	}
	schema, ok := structured["json"].(map[string]any)
	if !ok || schema["type"] != "object" {
		t.Fatalf("schema not preserved: %#v", structured["json"])
	}
	instr, _ := payload["instructions"].(string)
	if !strings.Contains(instr, "Be helpful.") || !strings.Contains(instr, "agent_plan") || !strings.Contains(instr, `"ok"`) {
		t.Fatalf("instructions should keep original text and schema guidance: %q", instr)
	}
	if payload["stream"] != true {
		t.Fatal("stream must remain unchanged")
	}
}

func TestNormalizeResponsesJSONObjectPassthrough(t *testing.T) {
	in := []byte(`{"input":"hi","text":{"format":{"type":"json_object"}},"stream":true}`)
	out, changed := normalizeOpenAIUpstreamBody("/v1/responses", in, openaiCompatConfig{})
	if changed {
		t.Fatalf("json_object must pass through unchanged: %s", out)
	}
}

func TestNormalizeResponsesTextPassthrough(t *testing.T) {
	in := []byte(`{"input":"hi","text":{"format":{"type":"text"}},"stream":true}`)
	_, changed := normalizeOpenAIUpstreamBody("/v1/responses", in, openaiCompatConfig{})
	if changed {
		t.Fatal("text format must pass through")
	}
}

func TestNormalizeResponsesJSONSchemaWithoutSchemaFallsBackToJSONObject(t *testing.T) {
	in := []byte(`{"input":"hi","text":{"format":{"type":"json_schema","name":"x"}},"stream":true}`)
	out, changed := normalizeOpenAIUpstreamBody("/v1/responses", in, openaiCompatConfig{})
	if !changed {
		t.Fatal("expected normalization")
	}
	var payload map[string]any
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatal(err)
	}
	if _, ok := payload["structured_outputs"]; ok {
		t.Fatal("structured_outputs requires a schema body")
	}
	text := payload["text"].(map[string]any)
	format := text["format"].(map[string]any)
	if format["type"] != "json_object" {
		t.Fatalf("fallback=%v", format)
	}
}

func TestNormalizeIgnoresNonResponsesPaths(t *testing.T) {
	in := []byte(`{"messages":[],"response_format":{"type":"json_schema","json_schema":{"name":"x","schema":{"type":"object"}}}}`)
	_, changed := normalizeOpenAIUpstreamBody("/v1/chat/completions", in, openaiCompatConfig{})
	if changed {
		t.Fatal("chat completions path is unchanged in this shim")
	}
}

func TestNormalizeNestedChatStyleSchemaInsideFormat(t *testing.T) {
	in := []byte(`{
		"input":"hi",
		"text":{"format":{"type":"json_schema","json_schema":{"name":"n","schema":{"type":"object","properties":{"a":{"type":"string"}}}}}}
	}`)
	out, changed := normalizeOpenAIUpstreamBody("/v1/responses", in, openaiCompatConfig{})
	if !changed {
		t.Fatal("expected normalization")
	}
	var payload map[string]any
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatal(err)
	}
	schema := payload["structured_outputs"].(map[string]any)["json"].(map[string]any)
	if schema["type"] != "object" {
		t.Fatalf("nested schema not extracted: %#v", schema)
	}
}

func TestNormalizeKeepsExistingStructuredOutputs(t *testing.T) {
	in := []byte(`{
		"input":"hi",
		"structured_outputs":{"json":{"type":"object","properties":{"b":{"type":"number"}}}},
		"text":{"format":{"type":"json_schema","name":"x","schema":{"type":"object","properties":{"a":{"type":"string"}}}}}
	}`)
	out, changed := normalizeOpenAIUpstreamBody("/v1/responses", in, openaiCompatConfig{})
	if !changed {
		t.Fatal("expected normalization")
	}
	var payload map[string]any
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatal(err)
	}
	if _, ok := payload["text"]; ok {
		t.Fatal("text.format must be removed")
	}
	schema := payload["structured_outputs"].(map[string]any)["json"].(map[string]any)
	if _, ok := schema["properties"].(map[string]any)["b"]; !ok {
		t.Fatalf("existing structured_outputs must be preserved: %#v", schema)
	}
}

func TestNormalizePreservesToolsAndStream(t *testing.T) {
	// Coding agents on wire_api=responses commonly send tools + json_schema together.
	in := []byte(`{
		"model":"qwen3.6",
		"input":[{"role":"user","content":"list files"}],
		"stream":true,
		"tool_choice":"auto",
		"tools":[{"type":"function","name":"shell","parameters":{"type":"object","properties":{"command":{"type":"string"}}}}],
		"text":{"format":{"type":"json_schema","name":"plan","schema":{"type":"object","properties":{"steps":{"type":"array"}},"required":["steps"]}}}
	}`)
	out, changed := normalizeOpenAIUpstreamBody("/v1/responses", in, openaiCompatConfig{})
	if !changed {
		t.Fatal("expected normalization")
	}
	var payload map[string]any
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatal(err)
	}
	if _, ok := payload["text"]; ok {
		t.Fatal("json_schema text.format must be stripped")
	}
	if _, ok := payload["structured_outputs"]; ok {
		t.Fatal("structured_outputs must not be applied when tools are present")
	}
	if payload["stream"] != true || payload["tool_choice"] != "auto" {
		t.Fatalf("stream/tool_choice changed: %#v", payload)
	}
	tools, ok := payload["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("tools not preserved: %#v", payload["tools"])
	}
	instr, _ := payload["instructions"].(string)
	if !strings.Contains(instr, "plan") || !strings.Contains(instr, "steps") {
		t.Fatalf("schema guidance missing from instructions: %q", instr)
	}
}

func TestPrepareOpenAIProxyRequestRewritesBody(t *testing.T) {
	body := `{"input":"hi","stream":true,"text":{"format":{"type":"json_schema","name":"n","schema":{"type":"object"}}}}`
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if err := prepareOpenAIProxyRequest(req, openaiCompatConfig{}); err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if _, ok := payload["structured_outputs"]; !ok {
		t.Fatalf("expected rewrite: %s", raw)
	}
	if req.ContentLength != int64(len(raw)) {
		t.Fatalf("ContentLength=%d len=%d", req.ContentLength, len(raw))
	}
}

func TestNormalizeCPUDisablesStructuredOutputs(t *testing.T) {
	in := []byte(`{
		"input":"hi",
		"structured_outputs":{"json":{"type":"object","properties":{"a":{"type":"string"}}}},
		"text":{"format":{"type":"json_schema","name":"n","schema":{"type":"object","properties":{"a":{"type":"string"}}}}}
	}`)
	out, changed := normalizeOpenAIUpstreamBody("/v1/responses", in, openaiCompatConfig{DisableStructuredOutputs: true})
	if !changed {
		t.Fatal("expected normalization")
	}
	var payload map[string]any
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatal(err)
	}
	if _, ok := payload["structured_outputs"]; ok {
		t.Fatalf("CPU must not forward structured_outputs: %#v", payload)
	}
	if _, ok := payload["text"]; ok {
		t.Fatal("json_schema text.format must still be stripped")
	}
	instr, _ := payload["instructions"].(string)
	if !strings.Contains(instr, `"a"`) {
		t.Fatalf("schema guidance should remain in instructions: %q", instr)
	}
}

func TestOpenaiCompatOptions(t *testing.T) {
	if !openaiCompatOptions(false).DisableStructuredOutputs {
		t.Fatal("CPU must disable structured outputs")
	}
	if openaiCompatOptions(true).DisableStructuredOutputs {
		t.Fatal("GPU may use structured outputs")
	}
}

func TestProxyOpenAINormalizesResponsesJSONSchema(t *testing.T) {
	var sawBody []byte
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: response.created\ndata: {}\n\n"))
	}))
	defer backend.Close()

	m := testManager(t)
	m.gpuProbe = func(context.Context) bool { return false }
	m.active = "ready-model"
	m.backend, _ = url.Parse(backend.URL)
	m.proxy = newOpenAIReverseProxy(m.backend)

	body := `{"model":"ready-model","input":"hi","stream":true,"text":{"format":{"type":"json_schema","name":"n","schema":{"type":"object","properties":{"a":{"type":"string"}}}}}}`
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	m.proxyOpenAI(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(sawBody, &payload); err != nil {
		t.Fatalf("backend body: %s err=%v", sawBody, err)
	}
	if _, ok := payload["text"]; ok {
		t.Fatalf("backend still received text.format: %s", sawBody)
	}
	// CPU must not enable structured_outputs (xgrammar pin_memory crash).
	if _, ok := payload["structured_outputs"]; ok {
		t.Fatalf("CPU backend must not receive structured_outputs: %s", sawBody)
	}
	instr, _ := payload["instructions"].(string)
	if !strings.Contains(instr, `"a"`) {
		t.Fatalf("schema guidance missing: %s", sawBody)
	}
}
