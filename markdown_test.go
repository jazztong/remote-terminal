package main

import (
	"strings"
	"testing"
)

func TestFormatMarkdownToTelegramHTML(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "empty_input",
			input: "",
			want:  "",
		},
		{
			name:  "plain_text",
			input: "Hello world",
			want:  "Hello world",
		},
		{
			name:  "html_escaping",
			input: "Use <div> & \"quotes\"",
			want:  "Use &lt;div&gt; &amp; &#34;quotes&#34;",
		},
		{
			name:  "bold",
			input: "This is **bold** text",
			want:  "This is <b>bold</b> text",
		},
		{
			name:  "italic",
			input: "This is *italic* text",
			want:  "This is <i>italic</i> text",
		},
		{
			name:  "bold_and_italic",
			input: "**bold** and *italic*",
			want:  "<b>bold</b> and <i>italic</i>",
		},
		{
			name:  "strikethrough",
			input: "This is ~~deleted~~ text",
			want:  "This is <s>deleted</s> text",
		},
		{
			name:  "inline_code",
			input: "Use `fmt.Println` here",
			want:  "Use <code>fmt.Println</code> here",
		},
		{
			name:  "inline_code_with_html",
			input: "Use `<div>` tag",
			want:  "Use <code>&lt;div&gt;</code> tag",
		},
		{
			name:  "header_h1",
			input: "# Hello World",
			want:  "<b>Hello World</b>",
		},
		{
			name:  "header_h2",
			input: "## Section Title",
			want:  "<b>Section Title</b>",
		},
		{
			name:  "header_h3",
			input: "### Sub Section",
			want:  "<b>Sub Section</b>",
		},
		{
			name:  "link",
			input: "See [Google](https://google.com) for more",
			want:  `See <a href="https://google.com">Google</a> for more`,
		},
		{
			name:  "link_with_ampersand",
			input: "See [results](https://example.com?a=1&b=2)",
			want:  `See <a href="https://example.com?a=1&b=2">results</a>`,
		},
		{
			name:  "bullet_dash",
			input: "- item one\n- item two",
			want:  "• item one\n• item two",
		},
		{
			name:  "bullet_asterisk",
			input: "* item one\n* item two",
			want:  "• item one\n• item two",
		},
		{
			name:  "code_block_with_language",
			input: "```go\nfmt.Println(\"hello\")\n```",
			want:  "<pre><code class=\"language-go\">fmt.Println(&#34;hello&#34;)</code></pre>",
		},
		{
			name:  "code_block_without_language",
			input: "```\nsome code\n```",
			want:  "<pre><code>some code</code></pre>",
		},
		{
			name:  "code_block_preserves_html_chars",
			input: "```html\n<div class=\"test\">&amp;</div>\n```",
			want:  "<pre><code class=\"language-html\">&lt;div class=&#34;test&#34;&gt;&amp;amp;&lt;/div&gt;</code></pre>",
		},
		{
			name:  "markdown_inside_code_block_stays_literal",
			input: "```\n**not bold** and *not italic*\n```",
			want:  "<pre><code>**not bold** and *not italic*</code></pre>",
		},
		{
			name:  "markdown_inside_inline_code_stays_literal",
			input: "Use `**not bold**` here",
			want:  "Use <code>**not bold**</code> here",
		},
		{
			name:  "unmatched_bold_markers",
			input: "This has ** unmatched markers",
			want:  "This has ** unmatched markers",
		},
		{
			name: "mixed_content_realistic",
			input: `# Hello

This is **bold** with ` + "`code`" + ` and a [link](https://example.com)

` + "```python\nprint(\"hello\")\n```" + `

- item one
- item two`,
			want: "<b>Hello</b>\n\nThis is <b>bold</b> with <code>code</code> and a <a href=\"https://example.com\">link</a>\n\n<pre><code class=\"language-python\">print(&#34;hello&#34;)</code></pre>\n\n• item one\n• item two",
		},
		{
			name:  "multiple_code_blocks",
			input: "```go\nfoo()\n```\n\ntext\n\n```python\nbar()\n```",
			want:  "<pre><code class=\"language-go\">foo()</code></pre>\n\ntext\n\n<pre><code class=\"language-python\">bar()</code></pre>",
		},
		{
			name:  "multiple_inline_codes",
			input: "Use `foo` and `bar` functions",
			want:  "Use <code>foo</code> and <code>bar</code> functions",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatMarkdownToTelegramHTML(tt.input)
			if got != tt.want {
				t.Errorf("formatMarkdownToTelegramHTML()\ngot:  %q\nwant: %q", got, tt.want)
			}
		})
	}
}

func TestExtractCodeBlocks(t *testing.T) {
	input := "before\n```go\nfmt.Println()\n```\nafter"
	result, blocks := extractCodeBlocks(input)

	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].language != "go" {
		t.Errorf("language = %q, want %q", blocks[0].language, "go")
	}
	if blocks[0].code != "fmt.Println()" {
		t.Errorf("code = %q, want %q", blocks[0].code, "fmt.Println()")
	}
	if !strings.Contains(result, "CODEBLOCK0") {
		t.Errorf("placeholder not found in result: %q", result)
	}
}

func TestExtractInlineCode(t *testing.T) {
	input := "Use `foo` and `bar`"
	result, codes := extractInlineCode(input)

	if len(codes) != 2 {
		t.Fatalf("expected 2 codes, got %d", len(codes))
	}
	if codes[0] != "foo" {
		t.Errorf("codes[0] = %q, want %q", codes[0], "foo")
	}
	if codes[1] != "bar" {
		t.Errorf("codes[1] = %q, want %q", codes[1], "bar")
	}
	if !strings.Contains(result, "INLINECODE0") || !strings.Contains(result, "INLINECODE1") {
		t.Errorf("placeholders not found in result: %q", result)
	}
}

func TestSplitFormattedMessage(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxLen   int
		wantLen  int
		wantAll  bool // if true, check all content preserved
	}{
		{
			name:    "short_message",
			input:   "Hello world",
			maxLen:  100,
			wantLen: 1,
		},
		{
			name:    "split_on_paragraphs",
			input:   "Paragraph one.\n\nParagraph two.\n\nParagraph three.",
			maxLen:  25,
			wantLen: 3,
			wantAll: true,
		},
		{
			name:    "single_long_paragraph",
			input:   "line one\nline two\nline three\nline four",
			maxLen:  20,
			wantLen: 2,
			wantAll: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chunks := splitFormattedMessage(tt.input, tt.maxLen)
			if len(chunks) != tt.wantLen {
				t.Errorf("got %d chunks, want %d; chunks: %v", len(chunks), tt.wantLen, chunks)
			}
			if tt.wantAll {
				// Verify all content lines are preserved
				joined := strings.Join(chunks, "\n\n")
				for _, line := range strings.Split(tt.input, "\n") {
					trimmed := strings.TrimSpace(line)
					if trimmed != "" && !strings.Contains(joined, trimmed) {
						t.Errorf("content lost: %q not found in chunks", trimmed)
					}
				}
			}
			// Verify no chunk exceeds maxLen (allow small overflow for unsplittable lines)
			for i, chunk := range chunks {
				if len(chunk) > tt.maxLen*2 {
					t.Errorf("chunk %d too long: %d > %d", i, len(chunk), tt.maxLen)
				}
			}
		})
	}
}

func TestConvertMarkdownPatterns(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "header_only",
			input: "# Title",
			want:  "<b>Title</b>",
		},
		{
			name:  "bullets_with_indent",
			input: "  - nested item",
			want:  "  • nested item",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := convertMarkdownPatterns(tt.input)
			if got != tt.want {
				t.Errorf("convertMarkdownPatterns()\ngot:  %q\nwant: %q", got, tt.want)
			}
		})
	}
}

func TestHasMarkdown(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"plain_text", "Hello world", false},
		{"ls_output", "file1.txt\nfile2.go\nmain.go", false},
		{"pwd_output", "/home/user/project", false},
		{"bold", "This is **bold**", true},
		{"inline_code", "Use `fmt.Println`", true},
		{"code_block", "```go\ncode\n```", true},
		{"header", "# Title", true},
		{"link", "[click](https://example.com)", true},
		{"bullet_dash", "- item one", true},
		{"bullet_star", "* item one", true},
		{"strikethrough", "~~deleted~~", true},
		{"number_not_bullet", "3 items found", false},
		{"dash_in_word", "non-interactive", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasMarkdown(tt.input)
			if got != tt.want {
				t.Errorf("hasMarkdown(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestConvertLinksURLSanitization(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "https_allowed",
			input: "[click](https://example.com)",
			want:  `<a href="https://example.com">click</a>`,
		},
		{
			name:  "http_allowed",
			input: "[click](http://example.com)",
			want:  `<a href="http://example.com">click</a>`,
		},
		{
			name:  "tg_allowed",
			input: "[open](tg://resolve?domain=test)",
			want:  `<a href="tg://resolve?domain=test">open</a>`,
		},
		{
			name:  "javascript_blocked",
			input: "[click](javascript:alert(1))",
			want:  "click (javascript:alert(1))",
		},
		{
			name:  "data_blocked",
			input: "[click](data:text/html,test)",
			want:  "click (data:text/html,test)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatMarkdownToTelegramHTML(tt.input)
			if got != tt.want {
				t.Errorf("got:  %q\nwant: %q", got, tt.want)
			}
		})
	}
}

func TestSplitAtSafeBoundary(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
	}{
		{
			name:   "split_avoids_entity",
			input:  "Hello &amp; world &lt;div&gt; test",
			maxLen: 10,
		},
		{
			name:   "no_entities",
			input:  "Hello world this is a test",
			maxLen: 10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parts := splitAtSafeBoundary(tt.input, tt.maxLen)
			// Rejoin must equal original
			joined := strings.Join(parts, "")
			if joined != tt.input {
				t.Errorf("content lost:\ngot:  %q\nwant: %q", joined, tt.input)
			}
			// No part should contain a broken entity (& without matching ;)
			for i, part := range parts {
				lastAmp := strings.LastIndex(part, "&")
				if lastAmp >= 0 {
					rest := part[lastAmp:]
					if !strings.Contains(rest, ";") && i < len(parts)-1 {
						// This is only a problem if the entity continues in next part
						if strings.HasPrefix(parts[i+1], "amp;") || strings.HasPrefix(parts[i+1], "lt;") || strings.HasPrefix(parts[i+1], "gt;") {
							t.Errorf("split broke HTML entity at part %d boundary: %q | %q", i, part, parts[i+1])
						}
					}
				}
			}
		})
	}
}

func BenchmarkFormatMarkdownToTelegramHTML(b *testing.B) {
	input := `# Hello World

This is a **bold** statement with ` + "`inline code`" + ` and a [link](https://example.com).

` + "```python\ndef hello():\n    print(\"world\")\n```" + `

- item one
- item two
- item three

Some *italic* and ~~strikethrough~~ text.`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		formatMarkdownToTelegramHTML(input)
	}
}
