package narrate

import (
	"regexp"
	"strings"
)

// The console renders model output as plain text, deliberately: rendering model-authored
// markdown would mean rendering model-authored structure, and this is a record-reading
// surface, not a document viewer. But a language model asked for plain prose still emits
// **bold** and ## headings intermittently, and "the prompt says not to" is not a guarantee.
//
// stripMarkdown is the guarantee. It removes formatting syntax while preserving every word,
// so a drifting model degrades to correct plain text rather than literal asterisks on screen.
var (
	// Headings: "## Summary" -> "Summary". Only at line start, so a "#1" mid-sentence lives.
	reHeading = regexp.MustCompile(`(?m)^[ \t]{0,3}#{1,6}[ \t]+`)
	// Bullets: "- point" / "* point" / "• point" -> "point".
	reBullet = regexp.MustCompile(`(?m)^[ \t]*[-*•][ \t]+`)
	// Blockquotes.
	reQuote = regexp.MustCompile(`(?m)^[ \t]*>[ \t]?`)
	// Fenced code delimiters.
	reFence = regexp.MustCompile("(?m)^[ \t]*```[a-zA-Z0-9]*[ \t]*$\n?")
	// **bold** and __bold__. Non-greedy, single-line, so unmatched markers are left alone
	// rather than swallowing the rest of the paragraph.
	reBold = regexp.MustCompile(`\*\*([^*\n]+)\*\*|__([^_\n]+)__`)
	// *italic*. Underscore italics are deliberately NOT stripped: identifiers like
	// end_to_end_id and remittance_info are real values in this domain and must survive.
	reItalic = regexp.MustCompile(`(^|[^*\w])\*([^*\n]+)\*([^*\w]|$)`)
	// `code`
	reCode = regexp.MustCompile("`([^`\n]+)`")
	// Horizontal rules. Spelled out per character because RE2 has no backreferences.
	reRule = regexp.MustCompile(`(?m)^[ \t]*(-[ \t]*-[ \t]*-[-  \t]*|\*[ \t]*\*[ \t]*\*[*  \t]*|_[ \t]*_[ \t]*_[_  \t]*)$\n?`)
	// Collapse the blank-line runs the removals leave behind.
	reBlank = regexp.MustCompile(`\n{3,}`)
)

// StripMarkdown returns s with markdown formatting syntax removed and all words intact.
func StripMarkdown(s string) string {
	s = reFence.ReplaceAllString(s, "")
	s = reRule.ReplaceAllString(s, "")
	s = reHeading.ReplaceAllString(s, "")
	s = reQuote.ReplaceAllString(s, "")
	s = reBullet.ReplaceAllString(s, "")
	s = reBold.ReplaceAllString(s, "$1$2")
	s = reItalic.ReplaceAllString(s, "$1$2$3")
	s = reCode.ReplaceAllString(s, "$1")
	s = reBlank.ReplaceAllString(s, "\n\n")

	// Trim trailing spaces the removals leave at line ends.
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// stripAll applies StripMarkdown across a slice, dropping entries that were only formatting.
func stripAll(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if c := StripMarkdown(s); c != "" {
			out = append(out, c)
		}
	}
	return out
}
