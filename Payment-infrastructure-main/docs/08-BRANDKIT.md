# 08 — Brand Kit (final)

Reference implementation: **`console-target-state.html`** — all 10 screens, desktop and mobile.
Build against the file; this document is the token reference and the rules.

Language: Mercury (surface, money typography, sidebar) + Unit21 (console structure, graph, case rail).

---

## 1 — Screen inventory

| # | Screen | Route | Primary content |
|---|---|---|---|
| 1 | Live Monitor | `/` | 4 metrics · area chart · session list · decisions table · ladder · dependencies |
| 2 | Investigation | `/case/:id` | Network graph · SHAP · four-state chips · linked txns · case rail |
| 3 | Resilience | `/resilience` | Dependency metrics · degradation ladder · shed/caps · event log |
| 4 | Audit Chain | `/audit` | Height/verify metrics · Merkle root · entry table with prev-hash links |
| 5 | Demo Runner | `/demo` | 8 scenario cards, each with expected outcome pill |
| 6 | Time Machine | `/time-machine` | Replay banner · persisted-vs-recomputed diff · replay history |
| 7 | Graph / Ring | `/graph` | Ring vs merchant control graphs side by side · component table |
| 8 | Calibration | `/calibration` | Reliability diagram · operating points · prevalence sensitivity · registry |
| 9 | Latency | `/latency` | Histogram with p99 marker · stage breakdown · load-test runs |
| 10 | Payer App | `/pay` (mobile) | Pay → Interstitial → Step-up → Result |

**Every nav item routes to its own screen.** No two nav entries share a target.

---

## 2 — Type

```html
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link rel="stylesheet" href="https://fonts.googleapis.com/css2?family=Geist:wght@400;500;600&family=Geist+Mono:wght@400;500&display=swap">
```

```css
--f:"Geist","Inter",-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;
--fm:"Geist Mono",ui-monospace,SFMono-Regular,monospace;
```

Mono is for IDs, hashes, timestamps, feature names and ring IDs **only**. Everything else is Geist.

| Use | Size / weight / tracking |
|---|---|
| Page title | 25 / 600 / −.025em |
| Card title | 15 / 600 / −.014em |
| Metric value | 27 / 600 / −.02em |
| Hero figure | 33 / 600 / −.028em |
| Payer amount | 38 / 600 / −.03em |
| Body, cell | 13.5 / 400 |
| Label, caption | 12–12.5 / 400 · `--ink3` |
| Status pill | 12 / 500 |
| Mono ID | 12 |

**Money always carries superscript decimals**, em-relative so it scales:

```css
.mny{font-weight:600;letter-spacing:-.02em;white-space:nowrap}
.mny sup{font-size:.62em;font-weight:500;color:var(--ink3);
  vertical-align:baseline;position:relative;top:-.52em;margin-left:.5px}
```

Format `en-IN` from `int64` paise at the render boundary — `₹4,28,600.00`, never `₹428,600.00`.
Strip figures use lakh/crore (`₹4.2L`), never K/M.

---

## 3 — Colour

```css
--canvas:#F7F8FA; --panel:#FFF; --hover:#F4F5F7; --sunken:#FAFBFC;
--bd:#E9EBF0; --bd2:#DFE2E9;
--ink:#0F172A; --ink2:#475467; --ink3:#667085; --ink4:#98A2B3;

--indigo:#4F46E5; --indigoh:#4338CA; --indigow:#EEF0FE;   /* primary          */
--teal:#2AA3B5; --tealw:#E6F5F7;                           /* graph payer nodes */
--coral:#F2695C; --coralw:#FEF0EE;                         /* flagged node      */
--navy:#0E3D54;                                            /* freeze action     */

--ok:#12805C;   --okw:#E7F5F0;     /* allowed  */
--warn:#B54708; --warnw:#FDF4E7;   /* step-up  */
--hold:#B93815; --holdw:#FDF1EC;   /* hold     */
--stop:#B42318; --stopw:#FEF1F0;   /* blocked  */

--sh:0 1px 2px rgba(16,24,40,.04),0 1px 3px rgba(16,24,40,.05);
--r:14px; --r2:10px; --r3:8px;
```

Indigo is **solid fill only** — primary button, active nav, chart line, ladder pip. Never a wash
behind body text (`--indigow` is for the active nav pill and selected row only).

Not used: gradients on surfaces · glassmorphism · dark mode · serif · uppercase-mono labels ·
green "verified" badge on an allow · spinners · emoji.

---

## 4 — Components

**Status pill** — tinted background + matching 6px dot + coloured text. Never bare coloured text.
Six variants: `s-ok · s-wn · s-hd · s-sp · s-nt · s-in`. `Capped` uses `s-nt` (grey) because a
regulatory ceiling is not a risk judgement. Degradation also uses `s-nt`, never red.

**Entity cell** — 29px initials circle + name (13.5/500/17px) + mono ID (11/14px) in a tight flex
column. Row padding 9px, total row height ≈48px. Never render a bare hash on both sides of a `→`.

**Metric card** — icon + label, value with superscript, footer row with delta text and a 62×22
sparkline.

**Decision ladder** — five rungs always rendered; landed rung indigo pip with a 3.5px `--indigow`
ring; dashed *advisory ceiling* divider read from `policy.advisory_max_rung`; `Cap` and `Block` as
pills below a rule, because they are not rungs.

**Four-state chips** — `✓ CLEAR` outline · `▲ FIRED` amber tint · `— NOT_APPLICABLE` dashed ·
`○ NOT_EVALUATED` diagonal hatch with reason appended. Four shapes, so the set survives greyscale.

**Case rail** — meta rows, then solid-fill workflow buttons: indigo escalate, green dismiss, navy
freeze, olive silence, coral confirm-fraud.

**Graph** — teal r21 payer nodes with white person glyphs, coral r28 flagged beneficiary, `#CFE3C4`
edges with `#EDF6E8` amount pills in `#3F7A2E` mono. Merchant control graph uses grey nodes and a
green centre to show ring_score 0.

**Charts** — indigo `2.2px` line, gradient area fill at 20%→0 opacity, `#EFF1F5` gridlines,
endpoint dot. Histograms use indigo bars with `--indigo-l` tail bars and a dashed amber p99 marker.

---

## 5 — Mobile (payer app)

Four screens: **Pay → Interstitial → Step-up → Result.**

- Frame 286px, `38px` outer radius, `30px` screen radius, notch pill.
- Amount 38/600/−.03em with superscript paise.
- Beneficiary card: 36px avatar + name (14/600) + VPA in mono 12.
- Interstitial: amber-tinted card, 34px warning icon, 17px headline, then **three plain-language facts** with icons — first-seen, fan-in, forwarding speed.
- Buttons 14px radius, 14px padding, 15/600. `Go back` is the dark weighted action; `Send anyway` is ghost. **The customer keeps the choice.**
- Step-up: six 52px OTP cells, filled cells indigo-tinted.
- Result: 62px tinted circle, 20px headline, and a **Report a problem** action — recovery is one tap.

**Copy rules, binding:**

| Never | Always |
|---|---|
| "created 3 days ago" | "we first saw this account 3 days ago" |
| "Verified" / green tick on an allow | nothing — an allow is the absence of a finding |
| "Step-up interstitial", "advisory", "ring_score" | "Take a look before you send" |
| "Fraud detected" | "This looks unusual" |

Zero engineering vocabulary on any payer surface.

---

## 6 — Layout

```css
.app{display:grid;grid-template-columns:240px 1fr;min-height:100vh}
.side{position:sticky;top:0;height:100vh;overflow-y:auto}
.top{position:sticky;top:0;z-index:20}
.page{padding:24px 26px 56px;max-width:1560px}
.screen{display:none}.screen.on{display:block}   /* never inline display:contents */
.sp1{grid-template-columns:1fr 328px;gap:14px}   /* main + rail */
.sp2{grid-template-columns:1fr 1fr;gap:14px}
@media(max-width:1200px){.sp1,.sp2{grid-template-columns:1fr}}
```

Card grids use `grid-auto-rows:1fr` so rows are never ragged. Tables sit flush to card edges with a
`--sunken` header row and sort carets on sortable columns.

---

## 7 — Done when

- [ ] All 10 nav items route to distinct screens
- [ ] Payer app implemented as its own responsive route, not a desktop card
- [ ] Every money figure has superscript decimals and `en-IN` grouping from paise
- [ ] Every status is a tinted pill with a dot
- [ ] Every account cell has avatar + name + mono ID; table rows ≈48px
- [ ] Latency shown as queue / service / total, total emphasised; `max` outliers captioned
- [ ] Decision ladder renders all five rungs plus the advisory ceiling
- [ ] Four chip states distinguishable in greyscale
- [ ] `Capped` and degradation are grey, never red or amber
- [ ] No serif, no dark mode, no gradients on surfaces, no spinners
- [ ] Fonts load from Google — check the network tab, Georgia must never appear
- [ ] `prefers-reduced-motion` honoured

---

**Back to:** [07-PRESENTATION-ARCHITECTURE.md](07-PRESENTATION-ARCHITECTURE.md)
