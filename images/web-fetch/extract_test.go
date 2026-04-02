package main

import (
	"strings"
	"testing"
)

func TestExtract_SimpleHTML(t *testing.T) {
	html := `<!DOCTYPE html>
<html lang="en">
<head>
  <title>Test Article</title>
  <meta name="description" content="A test description">
  <link rel="canonical" href="https://example.com/article">
</head>
<body>
  <article>
    <h1>Test Article Heading</h1>
    <p>This is a paragraph with some meaningful content that should be extracted by readability. It contains enough text to be considered a proper article with sufficient length for extraction purposes.</p>
    <p>Another paragraph with additional content to make the article long enough for readability to process it correctly and return useful results.</p>
  </article>
</body>
</html>`

	opts := ExtractOptions{
		IncludeLinks:    false,
		ContentType:     "text/html",
		MaxContentBytes: 0,
	}
	result, err := Extract([]byte(html), "https://example.com/article", opts)
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}

	if result.Metadata.Title == "" {
		t.Error("expected non-empty title")
	}
	if result.Metadata.Description != "A test description" {
		t.Errorf("expected description 'A test description', got %q", result.Metadata.Description)
	}
	if result.Metadata.Language != "en" {
		t.Errorf("expected language 'en', got %q", result.Metadata.Language)
	}
	if !strings.Contains(result.Content, "paragraph") {
		t.Errorf("expected content to contain 'paragraph', got: %s", result.Content)
	}
}

func TestExtract_PlainText(t *testing.T) {
	input := "Hello, world! This is plain text content."
	opts := ExtractOptions{
		ContentType:     "text/plain",
		MaxContentBytes: 0,
	}
	result, err := Extract([]byte(input), "", opts)
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}
	if result.Content != input {
		t.Errorf("expected plain text passthrough, got: %s", result.Content)
	}
}

func TestExtract_JSON(t *testing.T) {
	jsonInput := `{"name":"agency","version":1,"active":true}`
	opts := ExtractOptions{
		ContentType:     "application/json",
		MaxContentBytes: 0,
	}
	result, err := Extract([]byte(jsonInput), "", opts)
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}
	if !strings.HasPrefix(result.Content, "```json") {
		t.Errorf("expected content to start with ```json, got: %s", result.Content)
	}
	if !strings.Contains(result.Content, `"name"`) {
		t.Errorf("expected content to contain 'name' key, got: %s", result.Content)
	}
	if !strings.HasSuffix(strings.TrimSpace(result.Content), "```") {
		t.Errorf("expected content to end with ```, got: %s", result.Content)
	}
}

func TestExtract_Truncation(t *testing.T) {
	// Build a string longer than 1KB.
	long := strings.Repeat("abcdefghij", 200) // 2000 bytes
	opts := ExtractOptions{
		ContentType:     "text/plain",
		MaxContentBytes: 1024,
	}
	result, err := Extract([]byte(long), "", opts)
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}
	if !strings.Contains(result.Content, "[content truncated at 1KB limit]") {
		t.Errorf("expected truncation marker, got: %s", result.Content[:100])
	}
	if int64(len(result.Content)) > 1024+50 {
		t.Errorf("truncated content too long: %d bytes", len(result.Content))
	}
}

func TestExtract_MetadataOG(t *testing.T) {
	html := `<!DOCTYPE html>
<html lang="fr">
<head>
  <title>Fallback Title</title>
  <meta property="og:title" content="OG Title">
  <meta property="og:description" content="OG Description">
  <meta property="og:image" content="https://example.com/image.jpg">
  <meta property="article:published_time" content="2024-01-15T10:00:00Z">
</head>
<body>
  <p>Content here.</p>
</body>
</html>`

	opts := ExtractOptions{
		ContentType:     "text/html",
		MaxContentBytes: 0,
	}
	result, err := Extract([]byte(html), "https://example.com/page", opts)
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}

	if result.Metadata.Title != "OG Title" {
		t.Errorf("expected OG title 'OG Title', got %q", result.Metadata.Title)
	}
	if result.Metadata.OGImage != "https://example.com/image.jpg" {
		t.Errorf("expected OG image URL, got %q", result.Metadata.OGImage)
	}
	if result.Metadata.PublishedDate == "" {
		t.Error("expected non-empty published date")
	}
	if !strings.Contains(result.Metadata.PublishedDate, "2024-01-15") {
		t.Errorf("expected published date to contain '2024-01-15', got %q", result.Metadata.PublishedDate)
	}
}
