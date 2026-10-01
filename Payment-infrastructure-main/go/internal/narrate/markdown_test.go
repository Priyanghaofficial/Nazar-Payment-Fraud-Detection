package narrate

import "testing"

func TestStripMarkdownRemovesSyntaxKeepsWords(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"bold", "The system **capped** the payment.", "The system capped the payment."},
		{"underscore bold", "The system __capped__ the payment.", "The system capped the payment."},
		{"italic", "This is *unusual* for the payer.", "This is unusual for the payer."},
		{"heading", "## Summary\nThe payment was capped.", "Summary\nThe payment was capped."},
		{"deep heading", "###### Detail\nText.", "Detail\nText."},
		{"bullets", "- first point\n- second point", "first point\nsecond point"},
		{"asterisk bullets", "* first\n* second", "first\nsecond"},
		{"inline code", "The field `amount_minor` is in paise.", "The field amount_minor is in paise."},
		{"blockquote", "> quoted line", "quoted line"},
		{"fence", "```json\n{\"a\":1}\n```", "{\"a\":1}"},
		{"mixed", "## Why\n- **new** beneficiary\n- amount is *high*", "Why\nnew beneficiary\namount is high"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := StripMarkdown(c.in); got != c.want {
				t.Errorf("StripMarkdown(%q)\n got: %q\nwant: %q", c.in, got, c.want)
			}
		})
	}
}

// Domain values must survive. Feature IDs and event fields carry underscores, and rupee
// prose carries digits and punctuation that must not be treated as formatting.
func TestStripMarkdownPreservesDomainValues(t *testing.T) {
	for _, s := range []string{
		"The end_to_end_id is judge-1786-scam and remittance_info was ignored.",
		"Rule RF-001 fired; the amount was ₹2,916.65 over UPI.",
		"Signals RF-001, RF-002 and RAIL-102 all fired.",
	} {
		if got := StripMarkdown(s); got != s {
			t.Errorf("StripMarkdown mangled a domain value\n got: %q\nwant: %q", got, s)
		}
	}
}

// An unmatched marker must not swallow the remainder of the text.
func TestStripMarkdownLeavesUnmatchedMarkers(t *testing.T) {
	in := "The rate is 5* higher than baseline."
	if got := StripMarkdown(in); got != in {
		t.Errorf("got %q, want %q", got, in)
	}
}
