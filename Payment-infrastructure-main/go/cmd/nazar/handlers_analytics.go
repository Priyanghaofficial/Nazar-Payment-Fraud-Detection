package main

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"

	"nazar/internal/contract"
)

// GET /v1/analytics — "where is the fraud, and what kind is it?"
//
// Every number here is a group-by over decisions this system actually made. Nothing is
// pre-aggregated, nothing is seeded, and a cut with no data reports zero rather than being
// hidden — an empty heatmap on a fresh instance is the correct answer, not a bug.
//
// The cuts are chosen to answer the question an analyst or a regulator actually asks, which
// is never "what is the mean risk score". It is: which places are being hit, which attack
// shapes are landing, and where is the money going. Amount bands matter more than payment
// counts throughout, for the same reason the decision rule is expected-cost rather than a
// threshold — a thousand two-rupee probes are not the problem.

type cutRow struct {
	Key                string  `json:"key"`
	Label              string  `json:"label"`
	Total              int     `json:"total"`
	Challenged         int     `json:"challenged"`
	Rate               float64 `json:"rate"`
	ValueMinor         int64   `json:"value_minor"`
	ValueChallenged    int64   `json:"value_challenged_minor"`
	ValueRate          float64 `json:"value_rate"`
	// Present only for the geographic cut.
	Lat float64 `json:"lat,omitempty"`
	Lon float64 `json:"lon,omitempty"`
}

type analyticsResponse struct {
	WindowFrom   int64    `json:"window_from_ms"`
	WindowTo     int64    `json:"window_to_ms"`
	Decisions    int      `json:"decisions"`
	Challenged   int      `json:"challenged"`
	ValueMinor   int64    `json:"value_minor"`
	ValueStopped int64    `json:"value_challenged_minor"`
	Places       []cutRow `json:"places"`
	Typologies   []cutRow `json:"typologies"`
	Rails        []cutRow `json:"rails"`
	Bands        []cutRow `json:"bands"`
	Hours        []cutRow `json:"hours"`
	Signals      []cutRow `json:"signals"`
	Note         string   `json:"note"`
}

// amountBand groups by what the money means rather than by round numbers, because the bands
// are where the typologies live: probes, everyday spend, the impersonation-scam band, and
// the cash-out tier.
func amountBand(minor int64) (string, string, int) {
	switch {
	case minor < 10000:
		return "probe", "Under ₹100", 0
	case minor < 100000:
		return "everyday", "₹100 – ₹1,000", 1
	case minor < 500000:
		return "scamband", "₹1,000 – ₹5,000", 2
	case minor < 2500000:
		return "large", "₹5,000 – ₹25,000", 3
	default:
		return "cashout", "Over ₹25,000", 4
	}
}

// typologyOf names the attack shape from the id the simulator stamped, falling back to the
// broad source. This is a label on traffic we generated, never a claim about real-world
// fraud rates — the console renders it under a [RECOVERED] tier for exactly that reason.
func typologyOf(id string) (string, string) {
	rest, ok := strings.CutPrefix(id, "sim-atk-")
	if ok {
		for _, c := range availableCampaigns() {
			if strings.HasPrefix(rest, c.Kind+"-") {
				return c.Kind, c.Label
			}
		}
		return "attack", "Attack (unclassified)"
	}
	switch {
	case strings.HasPrefix(id, "judge-"):
		return "judge", "Live phone session"
	case strings.HasPrefix(id, "sim-amb-"):
		return "ambient", "Ordinary traffic"
	case strings.HasPrefix(id, "demo-"):
		return "demo", "Scripted scenario"
	}
	return "api", "Direct API"
}

func (s *Server) handleAnalytics(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), `
		SELECT d.end_to_end_id, d.action, d.findings,
		       extract(epoch from d.decided_at)*1000,
		       t.amount_minor, t.rail, coalesce(t.geo_cell,'')
		FROM decisions d
		JOIN transactions t ON t.end_to_end_id = d.end_to_end_id
		WHERE d.kind = 'LIVE' AND d.end_to_end_id NOT LIKE '%%-seed%%'
		ORDER BY d.decided_at DESC
		LIMIT 20000`)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	type agg struct {
		label                    string
		order                    int
		total, challenged        int
		value, valueChallenged   int64
	}
	places := map[string]*agg{}
	typos := map[string]*agg{}
	rails := map[string]*agg{}
	bands := map[string]*agg{}
	hours := map[string]*agg{}
	signals := map[string]*agg{}

	bump := func(m map[string]*agg, key, label string, order int, challenged bool, amount int64) {
		a, ok := m[key]
		if !ok {
			a = &agg{label: label, order: order}
			m[key] = a
		}
		a.total++
		a.value += amount
		if challenged {
			a.challenged++
			a.valueChallenged += amount
		}
	}

	var total, challengedN int
	var valueAll, valueStopped int64
	var minMs, maxMs int64

	for rows.Next() {
		var id, action, rail, geo string
		var findingsJSON []byte
		var decidedMs float64
		var amount int64
		if err := rows.Scan(&id, &action, &findingsJSON, &decidedMs, &amount, &rail, &geo); err != nil {
			continue
		}
		ms := int64(decidedMs)
		if minMs == 0 || ms < minMs {
			minMs = ms
		}
		if ms > maxMs {
			maxMs = ms
		}

		ch := action != string(contract.ActionAllow) && action != string(contract.ActionAllowMonitor)
		total++
		valueAll += amount
		if ch {
			challengedN++
			valueStopped += amount
		}

		if c, ok := cityOf(geo); ok {
			bump(places, c, c, 0, ch, amount)
		} else {
			bump(places, "unknown", "Location not recorded", 99, ch, amount)
		}

		tk, tl := typologyOf(id)
		bump(typos, tk, tl, 0, ch, amount)

		if rail == "" {
			rail = "UNKNOWN"
		}
		bump(rails, rail, rail, 0, ch, amount)

		bk, bl, bo := amountBand(amount)
		bump(bands, bk, bl, bo, ch, amount)

		h := time.UnixMilli(ms).Hour()
		bump(hours, pad2(h), pad2(h)+":00", h, ch, amount)

		// Which detector families are actually carrying the load.
		var fs []contract.Finding
		if json.Unmarshal(findingsJSON, &fs) == nil {
			seen := map[string]bool{}
			for _, f := range fs {
				if f.Status != contract.StatusFired {
					continue
				}
				if d := detectorOf(f.SignalID); d != "" && !seen[d] {
					seen[d] = true
					bump(signals, d, d, 0, ch, amount)
				}
			}
		}
	}

	toRows := func(m map[string]*agg, byOrder bool, withGeo bool) []cutRow {
		out := make([]cutRow, 0, len(m))
		for k, a := range m {
			row := cutRow{
				Key: k, Label: a.label, Total: a.total, Challenged: a.challenged,
				ValueMinor: a.value, ValueChallenged: a.valueChallenged,
			}
			if a.total > 0 {
				row.Rate = float64(a.challenged) / float64(a.total)
			}
			if a.value > 0 {
				row.ValueRate = float64(a.valueChallenged) / float64(a.value)
			}
			if withGeo {
				row.Lat, row.Lon = cityLatLon(a.label)
			}
			out = append(out, row)
		}
		sort.Slice(out, func(i, j int) bool {
			if byOrder {
				return m[out[i].Key].order < m[out[j].Key].order
			}
			if out[i].Challenged != out[j].Challenged {
				return out[i].Challenged > out[j].Challenged
			}
			return out[i].Total > out[j].Total
		})
		return out
	}

	writeJSON(w, http.StatusOK, analyticsResponse{
		WindowFrom: minMs, WindowTo: maxMs,
		Decisions: total, Challenged: challengedN,
		ValueMinor: valueAll, ValueStopped: valueStopped,
		Places:     toRows(places, false, true),
		Typologies: toRows(typos, false, false),
		Rails:      toRows(rails, false, false),
		Bands:      toRows(bands, true, false),
		Hours:      toRows(hours, true, false),
		Signals:    toRows(signals, false, false),
		Note: "Group-by over decisions this instance actually made. Attack-type labels describe traffic this system generated, so they measure the pipeline, not real-world fraud rates. Value is weighted throughout because a thousand two-rupee probes are not the problem a bank has.",
	})
}

func pad2(h int) string {
	if h < 10 {
		return "0" + string(rune('0'+h))
	}
	return string(rune('0'+h/10)) + string(rune('0'+h%10))
}

// GET /v1/graph/top — beneficiaries the in-process graph currently holds, ranked by distinct
// payers. Backs the account picker on the Graph/Ring screen so every account it offers has
// something to show, rather than sending an analyst to a screen of zeros for an account the
// graph has since decayed away.
func (s *Server) handleGraphTop(w http.ResponseWriter, r *http.Request) {
	top := s.graph.TopPayees(time.Now().UnixMilli(), 24)
	writeJSON(w, http.StatusOK, map[string]any{
		"accounts": top,
		"note":     "Held in process, so this reflects what has been seen since this instance started rather than all history. Ranked by distinct payers inside the decay window — the axis the ring score is built on.",
	})
}
