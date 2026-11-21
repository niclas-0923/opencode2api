package protocol

import (
	"encoding/json"
	"errors"
	"testing"
)

func decodeBody(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("decode converted body: %v (body %s)", err, data)
	}
	return out
}

func lookup(t *testing.T, value any, path ...string) any {
	t.Helper()
	current := value
	for _, key := range path {
		object, ok := current.(map[string]any)
		if !ok {
			t.Fatalf("%v is not an object while reading %q", current, key)
		}
		current = object[key]
	}
	return current
}

func stringAt(t *testing.T, value any, path ...string) string {
	t.Helper()
	got, ok := lookup(t, value, path...).(string)
	if !ok {
		t.Fatalf("%v is not a string at %v", lookup(t, value, path...), path)
	}
	return got
}

func numberAt(t *testing.T, value any, path ...string) float64 {
	t.Helper()
	got, ok := lookup(t, value, path...).(float64)
	if !ok {
		t.Fatalf("%v is not a number at %v", lookup(t, value, path...), path)
	}
	return got
}

// TestConvertChatResponseToAnthropic checks text, ID, stop reason and usage
// mapping for a plain assistant answer.
func TestConvertChatResponseToAnthropic(t *testing.T) {
	body := []byte(`{
		"id": "abc",
		"object": "chat.completion",
		"created": 1700000000,
		"model": "model-x",
		"choices": [{"index": 0, "message": {"role": "assistant", "content": "Hello"}, "finish_reason": "stop"}],
		"usage": {"prompt_tokens": 10, "completion_tokens": 4, "total_tokens": 14}
	}`)
	converted, err := ConvertResponse(Chat, Anthropic, body)
	if err != nil {
		t.Fatalf("ConvertResponse: %v", err)
	}
	out := decodeBody(t, converted)
	if got := stringAt(t, out, "id"); got != "msg_abc" {
		t.Fatalf("id = %q, want msg_abc", got)
	}
	if got := stringAt(t, out, "type"); got != "message" {
		t.Fatalf("type = %q, want message", got)
	}
	if got := stringAt(t, out, "stop_reason"); got != "end_turn" {
		t.Fatalf("stop_reason = %q, want end_turn", got)
	}
	blocks := lookup(t, out, "content").([]any)
	if len(blocks) != 1 || stringAt(t, blocks[0], "type") != "text" || stringAt(t, blocks[0], "text") != "Hello" {
		t.Fatalf("content = %v, want one text block Hello", blocks)
	}
	if got := numberAt(t, out, "usage", "input_tokens"); got != 10 {
		t.Fatalf("input_tokens = %v, want 10", got)
	}
	if got := numberAt(t, out, "usage", "output_tokens"); got != 4 {
		t.Fatalf("output_tokens = %v, want 4", got)
	}
}

// TestConvertAnthropicResponseToChat checks the reverse mapping, including the
// canonical stop reason and the summed usage total.
func TestConvertAnthropicResponseToChat(t *testing.T) {
	body := []byte(`{
		"id": "msg_xyz",
		"type": "message",
		"role": "assistant",
		"model": "model-y",
		"content": [{"type": "text", "text": "Hi"}],
		"stop_reason": "end_turn",
		"usage": {"input_tokens": 7, "output_tokens": 3}
	}`)
	converted, err := ConvertResponse(Anthropic, Chat, body)
	if err != nil {
		t.Fatalf("ConvertResponse: %v", err)
	}
	out := decodeBody(t, converted)
	if got := stringAt(t, out, "id"); got != "chatcmpl_xyz" {
		t.Fatalf("id = %q, want chatcmpl_xyz", got)
	}
	if got := stringAt(t, lookup(t, out, "choices").([]any)[0], "message", "content"); got != "Hi" {
		t.Fatalf("content = %q, want Hi", got)
	}
	if got := stringAt(t, lookup(t, out, "choices").([]any)[0], "finish_reason"); got != "stop" {
		t.Fatalf("finish_reason = %q, want stop", got)
	}
	if got := numberAt(t, out, "usage", "prompt_tokens"); got != 7 {
		t.Fatalf("prompt_tokens = %v, want 7", got)
	}
	if got := numberAt(t, out, "usage", "total_tokens"); got != 10 {
		t.Fatalf("total_tokens = %v, want 10", got)
	}
}

// TestConvertToolCallResponse checks that a Chat tool call becomes an Anthropic
// tool_use block with parsed input and a tool_use stop reason.
func TestConvertToolCallResponse(t *testing.T) {
	body := []byte(`{
		"id": "call-turn",
		"created": 1700000001,
		"model": "model-z",
		"choices": [{"index": 0, "message": {"role": "assistant", "content": null, "tool_calls": [
			{"id": "call_1", "type": "function", "function": {"name": "get_weather", "arguments": "{\"city\":\"Paris\"}"}}
		]}, "finish_reason": "tool_calls"}]
	}`)
	converted, err := ConvertResponse(Chat, Anthropic, body)
	if err != nil {
		t.Fatalf("ConvertResponse: %v", err)
	}
	out := decodeBody(t, converted)
	blocks := lookup(t, out, "content").([]any)
	if len(blocks) != 1 {
		t.Fatalf("content = %v, want one tool_use block", blocks)
	}
	block := blocks[0]
	if stringAt(t, block, "type") != "tool_use" || stringAt(t, block, "name") != "get_weather" {
		t.Fatalf("block = %v, want tool_use get_weather", block)
	}
	if got := stringAt(t, lookup(t, block, "input"), "city"); got != "Paris" {
		t.Fatalf("input.city = %q, want Paris", got)
	}
	if got := stringAt(t, out, "stop_reason"); got != "tool_use" {
		t.Fatalf("stop_reason = %q, want tool_use", got)
	}
}

// TestConvertResponseErrors checks that an upstream error envelope is not
// converted into an empty answer.
func TestConvertResponseErrors(t *testing.T) {
	_, err := ConvertResponse(Chat, Anthropic, []byte(`{"error":{"message":"boom"}}`))
	var failure *StreamFailure
	if !errors.As(err, &failure) || failure.Type != "upstream_error" || failure.Message != "boom" {
		t.Fatalf("error envelope returned %v, want upstream_error boom", err)
	}
	if _, err := ConvertResponse(Chat, Anthropic, []byte(`not json`)); err == nil {
		t.Fatal("invalid JSON was accepted")
	}
}

// TestConvertResponseSameProtocolCopies checks the identity conversion returns
// an independent copy.
func TestConvertResponseSameProtocolCopies(t *testing.T) {
	body := []byte(`{"id":"same"}`)
	converted, err := ConvertResponse(Chat, Chat, body)
	if err != nil {
		t.Fatalf("ConvertResponse: %v", err)
	}
	converted[0] = '['
	if string(body) != `{"id":"same"}` {
		t.Fatalf("identity conversion shared the input buffer: %s", body)
	}
}
