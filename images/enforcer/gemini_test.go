package main

import (
	"encoding/json"
	"testing"
)

func TestTranslateToGeminiNative(t *testing.T) {
	body := []byte(`{
		"model":"gemini-2.5-flash",
		"messages":[
			{"role":"system","content":"Be concise."},
			{"role":"user","content":"Who won Euro 2024?"}
		],
		"tools":[{"type":"web_search"},{"url_context":{}}],
		"temperature":0.2,
		"max_tokens":128
	}`)

	out, err := translateToGemini(body)
	if err != nil {
		t.Fatalf("translateToGemini failed: %v", err)
	}
	var got map[string]interface{}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if got["model"] != nil {
		t.Fatalf("native Gemini body should not include model: %#v", got)
	}
	if got["system_instruction"] == nil {
		t.Fatalf("missing system_instruction: %#v", got)
	}
	tools := got["tools"].([]interface{})
	if len(tools) != 2 {
		t.Fatalf("tools len = %d, want 2: %#v", len(tools), tools)
	}
	if _, ok := tools[0].(map[string]interface{})["google_search"]; !ok {
		t.Fatalf("web_search not mapped to google_search: %#v", tools[0])
	}
	if _, ok := tools[1].(map[string]interface{})["url_context"]; !ok {
		t.Fatalf("url_context not preserved: %#v", tools[1])
	}
}

func TestTranslateFromGeminiNative(t *testing.T) {
	body := []byte(`{
		"candidates":[{
			"content":{"parts":[{"text":"Spain won Euro 2024."}],"role":"model"},
			"groundingMetadata":{"webSearchQueries":["who won euro 2024"]}
		}],
		"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":5,"totalTokenCount":15}
	}`)
	out, err := translateFromGemini(body)
	if err != nil {
		t.Fatalf("translateFromGemini failed: %v", err)
	}
	var got map[string]interface{}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	choices := got["choices"].([]interface{})
	choice := choices[0].(map[string]interface{})
	msg := choice["message"].(map[string]interface{})
	if msg["content"] != "Spain won Euro 2024." {
		t.Fatalf("content = %v", msg["content"])
	}
	if msg["stop_reason"] != "end_turn" {
		t.Fatalf("message stop_reason = %v", msg["stop_reason"])
	}
	if choice["stop_reason"] != "end_turn" {
		t.Fatalf("choice stop_reason = %v", choice["stop_reason"])
	}
	usage := got["usage"].(map[string]interface{})
	if usage["prompt_tokens"].(float64) != 10 {
		t.Fatalf("usage not mapped: %#v", usage)
	}
}

func TestTranslateFromGeminiResponse(t *testing.T) {
	body := []byte(`{
		"candidates":[{"content":{"parts":[{"text":"A response body."}],"role":"model"}}],
		"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":5,"totalTokenCount":15}
	}`)
	out, err := translateFromGeminiResponse(body)
	if err != nil {
		t.Fatalf("translateFromGeminiResponse failed: %v", err)
	}
	var got map[string]interface{}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if got["object"] != "response" {
		t.Fatalf("object = %v", got["object"])
	}
	output := got["output"].([]interface{})
	msg := output[0].(map[string]interface{})
	content := msg["content"].([]interface{})
	text := content[0].(map[string]interface{})["text"]
	if text != "A response body." {
		t.Fatalf("text = %v", text)
	}
}
