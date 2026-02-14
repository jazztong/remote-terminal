package main

import (
	"fmt"
	"html"
	"math/rand"
	"regexp"
	"strings"
)

// Compiled regexes for markdown patterns (compiled once at package init)
var (
	reCodeBlock     = regexp.MustCompile("(?s)```(\\w*)\\n(.*?)\\n```")
	reInlineCode    = regexp.MustCompile("`([^`\\n]+)`")
	reHeader        = regexp.MustCompile("(?m)^#{1,6}\\s+(.+)$")
	reBullet        = regexp.MustCompile("(?m)^(\\s*)[-*]\\s+")
	reLink          = regexp.MustCompile("\\[([^\\]]+)\\]\\(([^)]+)\\)")
	reBold          = regexp.MustCompile("\\*\\*(.+?)\\*\\*")
	reStrikethrough = regexp.MustCompile("~~(.+?)~~")
	reItalic        = regexp.MustCompile("(?:^|[^*])\\*([^*\\n]+?)\\*(?:[^*]|$)")
)

// placeholderPrefix is a random token generated at init to prevent collisions
// with actual content. Using §...§ alone could match real program output.
var placeholderPrefix = fmt.Sprintf("__PH%06d__", rand.Intn(999999))

// codeBlock holds extracted fenced code block info
type codeBlock struct {
	language string
	code     string
}

// hasMarkdown returns true if the text contains markdown patterns worth converting.
// Uses cheap string checks to avoid running the full regex pipeline on plain text
// (e.g., ls output, pwd, simple command results).
func hasMarkdown(s string) bool {
	if strings.Contains(s, "```") ||
		strings.Contains(s, "**") ||
		strings.Contains(s, "~~") ||
		strings.ContainsRune(s, '`') ||
		strings.Contains(s, "](") {
		return true
	}
	// Check for headers, bullets, or italic markers line by line
	for _, line := range strings.SplitN(s, "\n", 20) {
		trimmed := strings.TrimSpace(line)
		if len(trimmed) < 2 {
			continue
		}
		// Headers: #, ##, ###, etc.
		if trimmed[0] == '#' {
			return true
		}
		// Bullets: "- item" or "* item"
		if (trimmed[0] == '-' || trimmed[0] == '*') && trimmed[1] == ' ' {
			return true
		}
		// Italic: *word* (lone asterisks not part of **)
		if strings.ContainsRune(trimmed, '*') {
			return true
		}
	}
	return false
}

// formatMarkdownToTelegramHTML converts markdown text to Telegram-compatible HTML.
// Processes in phases to avoid converting markdown inside code blocks:
//  1. Extract fenced code blocks → placeholders
//  2. Extract inline code → placeholders
//  3. HTML-escape remaining text
//  4. Convert markdown patterns (headers, bold, italic, links, bullets)
//  5. Restore placeholders with proper HTML tags
func formatMarkdownToTelegramHTML(input string) string {
	if input == "" {
		return ""
	}

	// Skip the regex pipeline for plain text (command output, etc.)
	if !hasMarkdown(input) {
		return html.EscapeString(input)
	}

	// Phase 1: Extract fenced code blocks
	text, blocks := extractCodeBlocks(input)

	// Phase 2: Extract inline code
	text, inlineCodes := extractInlineCode(text)

	// Phase 3: HTML-escape remaining text
	text = html.EscapeString(text)

	// Phase 4: Convert markdown patterns
	text = convertMarkdownPatterns(text)

	// Phase 5: Restore placeholders
	text = restoreCodeBlocks(text, blocks)
	text = restoreInlineCode(text, inlineCodes)

	return text
}

// extractCodeBlocks replaces fenced code blocks (```lang\n...\n```) with
// numbered placeholders and returns the extracted blocks.
func extractCodeBlocks(input string) (string, []codeBlock) {
	var blocks []codeBlock
	result := reCodeBlock.ReplaceAllStringFunc(input, func(match string) string {
		parts := reCodeBlock.FindStringSubmatch(match)
		blocks = append(blocks, codeBlock{
			language: parts[1],
			code:     parts[2],
		})
		return fmt.Sprintf("%sCODEBLOCK%d%s", placeholderPrefix, len(blocks)-1, placeholderPrefix)
	})
	return result, blocks
}

// extractInlineCode replaces inline code (`...`) with numbered placeholders.
func extractInlineCode(input string) (string, []string) {
	var codes []string
	result := reInlineCode.ReplaceAllStringFunc(input, func(match string) string {
		parts := reInlineCode.FindStringSubmatch(match)
		codes = append(codes, parts[1])
		return fmt.Sprintf("%sINLINECODE%d%s", placeholderPrefix, len(codes)-1, placeholderPrefix)
	})
	return result, codes
}

// convertMarkdownPatterns converts markdown syntax in HTML-escaped text.
// Order matters: headers and bullets (line-based) before inline patterns,
// bold before italic to avoid ** vs * conflicts.
func convertMarkdownPatterns(text string) string {
	// Headers: # Header → <b>Header</b>
	text = reHeader.ReplaceAllString(text, "<b>$1</b>")

	// Bullets: - item or * item → • item
	text = reBullet.ReplaceAllString(text, "${1}• ")

	// Links: [text](url) → <a href="url">text</a>
	text = convertLinks(text)

	// Bold: **text** → <b>text</b>
	text = reBold.ReplaceAllString(text, "<b>$1</b>")

	// Strikethrough: ~~text~~ → <s>text</s>
	text = reStrikethrough.ReplaceAllString(text, "<s>$1</s>")

	// Italic: *text* → <i>text</i> (after bold is removed)
	text = convertItalic(text)

	return text
}

// convertLinks handles [text](url) after HTML escaping.
// Only allows safe URL protocols (http, https, tg) to prevent
// javascript:/data: injection from malicious program output.
func convertLinks(text string) string {
	return reLink.ReplaceAllStringFunc(text, func(match string) string {
		parts := reLink.FindStringSubmatch(match)
		linkText := parts[1]
		url := parts[2]
		// Unescape &amp; back to & in URLs (HTML-escaping artifact)
		url = strings.ReplaceAll(url, "&amp;", "&")
		// Only allow safe URL protocols
		lower := strings.ToLower(url)
		if !strings.HasPrefix(lower, "http://") &&
			!strings.HasPrefix(lower, "https://") &&
			!strings.HasPrefix(lower, "tg://") {
			return linkText + " (" + url + ")"
		}
		return fmt.Sprintf(`<a href="%s">%s</a>`, url, linkText)
	})
}

// convertItalic handles *text* → <i>text</i>, carefully avoiding
// already-converted bold markers and bullet characters.
func convertItalic(text string) string {
	return reItalic.ReplaceAllStringFunc(text, func(match string) string {
		parts := reItalic.FindStringSubmatch(match)
		if len(parts) < 2 {
			return match
		}
		inner := parts[1]
		// Preserve leading/trailing context characters
		prefix := ""
		suffix := ""
		if len(match) > 0 && match[0] != '*' {
			prefix = string(match[0])
		}
		if len(match) > 0 && match[len(match)-1] != '*' {
			suffix = string(match[len(match)-1])
		}
		return prefix + "<i>" + inner + "</i>" + suffix
	})
}

// restoreCodeBlocks replaces placeholder tokens with HTML <pre><code> blocks.
func restoreCodeBlocks(text string, blocks []codeBlock) string {
	for i, block := range blocks {
		placeholder := fmt.Sprintf("%sCODEBLOCK%d%s", placeholderPrefix, i, placeholderPrefix)
		escaped := html.EscapeString(block.code)
		var replacement string
		if block.language != "" {
			replacement = fmt.Sprintf("<pre><code class=\"language-%s\">%s</code></pre>", block.language, escaped)
		} else {
			replacement = fmt.Sprintf("<pre><code>%s</code></pre>", escaped)
		}
		text = strings.Replace(text, placeholder, replacement, 1)
	}
	return text
}

// restoreInlineCode replaces placeholder tokens with HTML <code> tags.
func restoreInlineCode(text string, codes []string) string {
	for i, code := range codes {
		placeholder := fmt.Sprintf("%sINLINECODE%d%s", placeholderPrefix, i, placeholderPrefix)
		escaped := html.EscapeString(code)
		text = strings.Replace(text, placeholder, "<code>"+escaped+"</code>", 1)
	}
	return text
}

// splitFormattedMessage splits HTML-formatted text into chunks that fit
// within Telegram's message size limit. Splits on paragraph boundaries
// (\n\n) to avoid breaking mid-HTML-tag. Individual lines exceeding
// maxLen are force-split at maxLen while avoiding mid-HTML-entity breaks.
func splitFormattedMessage(formatted string, maxLen int) []string {
	if len(formatted) <= maxLen {
		return []string{formatted}
	}

	var chunks []string
	paragraphs := strings.Split(formatted, "\n\n")
	var current strings.Builder

	for _, para := range paragraphs {
		// If adding this paragraph would exceed the limit, flush current
		if current.Len() > 0 && current.Len()+2+len(para) > maxLen {
			chunks = append(chunks, strings.TrimSpace(current.String()))
			current.Reset()
		}

		// If a single paragraph exceeds maxLen, split it on single newlines
		if len(para) > maxLen {
			lines := strings.Split(para, "\n")
			for _, line := range lines {
				if current.Len() > 0 && current.Len()+1+len(line) > maxLen {
					chunks = append(chunks, strings.TrimSpace(current.String()))
					current.Reset()
				}
				// If a single line still exceeds maxLen, force-split it
				if len(line) > maxLen {
					subChunks := splitAtSafeBoundary(line, maxLen)
					for j, sc := range subChunks {
						if j == len(subChunks)-1 {
							// Last sub-chunk goes into current buffer
							if current.Len() > 0 {
								current.WriteString("\n")
							}
							current.WriteString(sc)
						} else {
							if current.Len() > 0 {
								current.WriteString("\n")
								current.WriteString(sc)
								chunks = append(chunks, strings.TrimSpace(current.String()))
								current.Reset()
							} else {
								chunks = append(chunks, strings.TrimSpace(sc))
							}
						}
					}
					continue
				}
				if current.Len() > 0 {
					current.WriteString("\n")
				}
				current.WriteString(line)
			}
			continue
		}

		if current.Len() > 0 {
			current.WriteString("\n\n")
		}
		current.WriteString(para)
	}

	if current.Len() > 0 {
		chunks = append(chunks, strings.TrimSpace(current.String()))
	}

	return chunks
}

// splitAtSafeBoundary splits a long string at maxLen boundaries while avoiding
// splits in the middle of HTML entities (e.g., &amp; &lt; &#34;).
func splitAtSafeBoundary(s string, maxLen int) []string {
	var parts []string
	for len(s) > maxLen {
		end := maxLen
		// Check if we're splitting inside an HTML entity (&...;)
		// Scan back from the split point to find any unclosed &
		for j := end - 1; j >= 0 && j >= end-10; j-- {
			if s[j] == ';' {
				break // Entity is complete before split point
			}
			if s[j] == '&' {
				// Found start of entity that extends past split — split before it
				end = j
				break
			}
		}
		parts = append(parts, s[:end])
		s = s[end:]
	}
	if len(s) > 0 {
		parts = append(parts, s)
	}
	return parts
}
