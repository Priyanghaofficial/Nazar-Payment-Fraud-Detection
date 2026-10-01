package narrate

import "testing"

// A model asked for JSON does not always return well-formed JSON. Each of these is a shape
// observed from, or plausibly produced by, a hosted endpoint; none of them may ever reach an
// analyst's screen as raw structure.
func TestCleanSummaryRecoversMalformedModelOutput(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"clean passes through",
			"The system capped a ₹9,000 UPI payment.",
			"The system capped a ₹9,000 UPI payment."},
		{"whole object serialised into the field",
			`{"summary":"Capped the payment.","reasoning":["a","b"],"next_steps":["c"]}`,
			"Capped the payment."},
		{"markdown fenced",
			"```json\n{\"summary\":\"Capped the payment.\"}\n```",
			"Capped the payment."},
		{"truncated object leaks the next key",
			`Capped the payment.","reasoning":["The system recorded an ALLOW`,
			"Capped the payment."},
		{"spaced key separator",
			`Capped the payment.", "next_steps":["review"]`,
			"Capped the payment."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := cleanSummary(c.in); got != c.want {
				t.Errorf("cleanSummary(%q)\n got: %q\nwant: %q", c.in, got, c.want)
			}
		})
	}
}

// Unrecoverable structure must yield empty, never raw JSON, so the caller falls back to a
// field it knows is prose.
func TestCleanSummaryRefusesToEmitRawJSON(t *testing.T) {
	for _, in := range []string{
		`{"reasoning":["no summary key at all"]}`,
		`{{"broken":`,
	} {
		if got := cleanSummary(in); got != "" {
			t.Errorf("cleanSummary(%q) = %q, want empty rather than structure", in, got)
		}
	}
}
