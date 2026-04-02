package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"
	readability "codeberg.org/readeck/go-readability"
)

// Metadata holds extracted page metadata.
type Metadata struct {
	Title         string
	Description   string
	Language      string
	PublishedDate string
	CanonicalURL  string
	OGImage       string
}

// ExtractResult holds the extracted content and metadata.
type ExtractResult struct {
	Content  string
	Metadata Metadata
}

// ExtractOptions controls extraction behaviour.
type ExtractOptions struct {
	IncludeLinks    bool
	ContentType     string
	MaxContentBytes int64
}

// Extract dispatches to the appropriate extractor based on content type.
func Extract(body []byte, pageURL string, opts ExtractOptions) (*ExtractResult, error) {
	ct := strings.ToLower(opts.ContentType)

	switch {
	case strings.Contains(ct, "text/html") || ct == "":
		return extractHTML(body, pageURL, opts)
	case strings.Contains(ct, "application/json"):
		return extractJSON(body, opts)
	default:
		return extractPlainText(body, ct, opts)
	}
}

// extractHTML uses go-readability for article extraction, html-to-markdown for conversion,
// and goquery for metadata extraction.
func extractHTML(body []byte, pageURL string, opts ExtractOptions) (*ExtractResult, error) {
	parsedURL, err := url.Parse(pageURL)
	if err != nil {
		parsedURL = &url.URL{}
	}

	// Extract metadata from raw HTML using goquery.
	meta := extractHTMLMetadata(body)

	// Replace <img> tags with [Image: alt text] before readability parsing.
	htmlForReadability := replaceImagesWithText(body)

	// Use go-readability to extract main article content.
	article, err := readability.FromReader(bytes.NewReader(htmlForReadability), parsedURL)
	if err != nil {
		// Fallback: convert whole HTML to markdown.
		md, convErr := htmltomarkdown.ConvertString(string(htmlForReadability))
		if convErr != nil {
			return nil, fmt.Errorf("extract html: %w", convErr)
		}
		if !opts.IncludeLinks {
			md = stripMarkdownLinks(md)
		}
		md = truncate(md, opts.MaxContentBytes)
		return &ExtractResult{Content: md, Metadata: meta}, nil
	}

	// Fill in metadata from article if not already populated.
	if meta.Title == "" {
		meta.Title = article.Title
	}
	if meta.Language == "" {
		meta.Language = article.Language
	}
	if meta.OGImage == "" {
		meta.OGImage = article.Image
	}
	if meta.PublishedDate == "" && article.PublishedTime != nil {
		meta.PublishedDate = article.PublishedTime.Format("2006-01-02")
	}

	// Convert article HTML content to markdown.
	md, err := htmltomarkdown.ConvertString(article.Content)
	if err != nil {
		md = article.TextContent
	}

	if !opts.IncludeLinks {
		md = stripMarkdownLinks(md)
	}

	md = truncate(md, opts.MaxContentBytes)
	return &ExtractResult{Content: md, Metadata: meta}, nil
}

// extractHTMLMetadata extracts metadata using goquery.
func extractHTMLMetadata(body []byte) Metadata {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return Metadata{}
	}

	var meta Metadata

	// Title: prefer og:title, fall back to <title>.
	ogTitle := doc.Find(`meta[property="og:title"]`).AttrOr("content", "")
	if ogTitle != "" {
		meta.Title = ogTitle
	} else {
		meta.Title = strings.TrimSpace(doc.Find("title").First().Text())
	}

	// Description: prefer og:description, fall back to meta[name=description].
	ogDesc := doc.Find(`meta[property="og:description"]`).AttrOr("content", "")
	if ogDesc != "" {
		meta.Description = ogDesc
	} else {
		meta.Description = doc.Find(`meta[name="description"]`).AttrOr("content", "")
	}

	// OG image.
	meta.OGImage = doc.Find(`meta[property="og:image"]`).AttrOr("content", "")

	// Published date.
	meta.PublishedDate = doc.Find(`meta[property="article:published_time"]`).AttrOr("content", "")

	// Language from <html lang="...">.
	meta.Language = doc.Find("html").AttrOr("lang", "")

	// Canonical URL.
	meta.CanonicalURL = doc.Find(`link[rel="canonical"]`).AttrOr("href", "")

	return meta
}

// replaceImagesWithText replaces <img> tags with [Image: alt text] placeholders.
func replaceImagesWithText(body []byte) []byte {
	imgRe := regexp.MustCompile(`(?i)<img[^>]*>`)
	altRe := regexp.MustCompile(`(?i)\balt="([^"]*)"`)

	result := imgRe.ReplaceAllFunc(body, func(tag []byte) []byte {
		m := altRe.FindSubmatch(tag)
		if m != nil && len(m[1]) > 0 {
			return []byte(fmt.Sprintf("[Image: %s]", m[1]))
		}
		return []byte("[Image]")
	})
	return result
}

// extractJSON pretty-prints JSON and wraps it in a ```json code fence.
func extractJSON(body []byte, opts ExtractOptions) (*ExtractResult, error) {
	var v interface{}
	if err := json.Unmarshal(body, &v); err != nil {
		// Not valid JSON; return raw content as plain text.
		content := truncate(string(body), opts.MaxContentBytes)
		return &ExtractResult{Content: content}, nil
	}

	pretty, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		content := truncate(string(body), opts.MaxContentBytes)
		return &ExtractResult{Content: content}, nil
	}

	content := "```json\n" + string(pretty) + "\n```"
	content = truncate(content, opts.MaxContentBytes)
	return &ExtractResult{Content: content}, nil
}

// extractPlainText wraps content in an appropriate code fence based on type.
func extractPlainText(body []byte, contentType string, opts ExtractOptions) (*ExtractResult, error) {
	text := string(body)
	var content string

	switch {
	case strings.Contains(contentType, "text/csv"):
		content = "```csv\n" + text + "\n```"
	case strings.Contains(contentType, "xml"):
		content = "```xml\n" + text + "\n```"
	default:
		content = text
	}

	content = truncate(content, opts.MaxContentBytes)
	return &ExtractResult{Content: content}, nil
}

// truncate truncates content to maxBytes, appending a marker if truncated.
// If maxBytes <= 0, no truncation is applied.
func truncate(content string, maxBytes int64) string {
	if maxBytes <= 0 {
		return content
	}
	if int64(len(content)) <= maxBytes {
		return content
	}
	kb := maxBytes / 1024
	marker := fmt.Sprintf("\n[content truncated at %dKB limit]", kb)
	cutAt := int(maxBytes) - len(marker)
	if cutAt < 0 {
		cutAt = 0
	}
	return content[:cutAt] + marker
}

// stripMarkdownLinks converts [text](url) to just text.
var mdLinkRe = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)

func stripMarkdownLinks(md string) string {
	return mdLinkRe.ReplaceAllString(md, "$1")
}
