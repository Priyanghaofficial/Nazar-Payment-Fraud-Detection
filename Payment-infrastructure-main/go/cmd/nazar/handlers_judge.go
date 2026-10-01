package main

import (
	"fmt"
	"math/rand/v2"
	"net/http"
	"time"

	"nazar/internal/contract"
)

// The judge-facing payer app (docs/08-BRANDKIT.md S0): a judge scans a QR on the console,
// lands on a mobile page, and makes real payments that flow through the exact same
// s.DecideAndPersist path as everything else. handleJudgeSession exists only to give that
// session a believable identity before the judge's own taps become live decisions — it
// seeds a fresh payer with real (small, unremarkable) transaction history via the same
// production-shaped path, so the first payment the judge makes isn't flagged purely for
// having zero history. Nothing about the SCORING of what the judge does next is scripted:
// their two taps hit POST /v1/decide for real and get whatever the engine actually decides.
//
// What IS varied per session is the *story*: which scam approaches the customer, in what
// words, for how much. A demo that tells the same KYC story every time reads as a recording
// even when it isn't, and the second judge in the room has already seen the ending. Each of
// these is a real, separately-documented Indian fraud typology, so rotating them widens what
// the demo demonstrates rather than just redecorating it.

// merchantAccounts are shared across all judge sessions on purpose — a real small merchant
// that's seen many distinct payers looks exactly like what it is (docs/06's
// test_merchant_is_not_a_ring point, reused here for realism, not just correctness).

type judgeScenario struct {
	Key string `json:"key"`
	// Who the judge is playing, and where they habitually pay.
	PersonaName     string `json:"persona_name"`
	PersonaBlurb    string `json:"persona_blurb"`
	MerchantAccount string `json:"merchant_account"`
	MerchantLabel   string `json:"merchant_label"`
	MerchantSub     string `json:"merchant_sub"`
	MerchantInitial string `json:"merchant_initials"`
	EverydayMinor   int64  `json:"everyday_amount_minor"`

	// The approach.
	ScamLabel     string `json:"scam_label"`
	ScamInitials  string `json:"scam_initials"`
	SenderID      string `json:"sender_id"`
	CallerNumber  string `json:"caller_number"`
	CallerCaption string `json:"caller_caption"`
	Headline      string `json:"headline"`
	MessageBody   string `json:"message_body"`
	AccountCaptn  string `json:"account_caption"`
	ScamMinor     int64  `json:"scam_amount_minor"`
	// Context the presenter can lean on, and the line that makes the fraud obvious in
	// hindsight. Shown after the decision, never before.
	WhyItWorks string `json:"why_it_works"`
	TheTruth   string `json:"the_truth"`
}

var judgeScenarios = []judgeScenario{
	{
		Key: "kyc", PersonaName: "Priya Sharma",
		PersonaBlurb:    "You've banked with us for two years. You pay for chai, you send money to family, and you have never once thought about fraud detection.",
		MerchantAccount: "BANK_A-JUDGE-CHAI-POINT", MerchantLabel: "Chai Point",
		MerchantSub: "Your usual morning stop", MerchantInitial: "CP", EverydayMinor: 12000,
		ScamLabel: "KYC Verification Cell", ScamInitials: "KV",
		SenderID: "VM-KYCVRF", CallerNumber: "+91 80 4718 2299", CallerCaption: "Missed call · 2 minutes ago",
		Headline:     "Your account KYC has expired",
		MessageBody:  "URGENT: Your account KYC has expired and will be suspended today at 6 PM. To keep your account active, transfer ₹3,000 to the verification account below. The amount is refunded within 24 hours.",
		AccountCaptn: "Verification A/C", ScamMinor: 300000,
		WhyItWorks: "A deadline you cannot verify, an authority you cannot call back, and a refund promise that costs the fraudster nothing to make.",
		TheTruth:   "No bank will ever ask you to move money into a \"verification\" account. Verification never involves a payment.",
	},
	{
		Key: "electricity", PersonaName: "Rakesh Menon",
		PersonaBlurb:    "You run a two-table tiffin place. You pay your suppliers by UPI every morning and your electricity bill from the same phone.",
		MerchantAccount: "BANK_A-JUDGE-VEG-MANDI", MerchantLabel: "Anand Vegetable Mandi",
		MerchantSub: "Daily supplier", MerchantInitial: "AV", EverydayMinor: 34000,
		ScamLabel: "Power Board Recovery", ScamInitials: "PB",
		SenderID: "AX-MSEBDU", CallerNumber: "+91 92 4410 8817", CallerCaption: "Missed call · just now",
		Headline:     "Power disconnection tonight",
		MessageBody:  "Dear consumer, your electricity connection will be DISCONNECTED tonight at 9:30 PM as your previous bill was not updated. Pay ₹4,500 immediately to the recovery account below to avoid disconnection. Ignore if already paid.",
		AccountCaptn: "Recovery A/C", ScamMinor: 450000,
		WhyItWorks: "It targets a small business at the hour when losing power means losing tomorrow's trade, so the victim pays before thinking.",
		TheTruth:   "A utility never collects a bill into a personal UPI handle, and never gives you ninety minutes to pay it.",
	},
	{
		Key: "refund", PersonaName: "Anita Desai",
		PersonaBlurb:    "You order groceries online, you split dinner bills with friends, and you have sent money to a stranger before — by mistake, once.",
		MerchantAccount: "BANK_A-JUDGE-FRESH-CART", MerchantLabel: "FreshCart Grocery",
		MerchantSub: "Weekly order", MerchantInitial: "FC", EverydayMinor: 86000,
		ScamLabel: "Ravi Kumar", ScamInitials: "RK",
		SenderID: "VK-UPIALT", CallerNumber: "+91 70 3388 1204", CallerCaption: "3 missed calls · 4 minutes ago",
		Headline:     "\"I sent you money by mistake\"",
		MessageBody:  "Sir I am so sorry, I typed the wrong UPI ID and sent ₹5,000 to your account instead of my son's school fees. Please check and return it, I am a poor man and that was all I had. Please sir, send it back to this number immediately.",
		AccountCaptn: "Return to A/C", ScamMinor: 500000,
		WhyItWorks: "Nothing was ever credited. The victim is paying out of their own balance to relieve a guilt the fraudster manufactured.",
		TheTruth:   "Check your own balance before returning anything. A real wrong transfer is reversed by the bank, not by you.",
	},
	{
		Key: "arrest", PersonaName: "Suresh Iyer",
		PersonaBlurb:    "You are recently retired. You pay your society maintenance on time, you keep your papers in order, and you have never been in trouble in your life.",
		MerchantAccount: "BANK_A-JUDGE-SOCIETY", MerchantLabel: "Green Acres Society",
		MerchantSub: "Monthly maintenance", MerchantInitial: "GA", EverydayMinor: 250000,
		ScamLabel: "Cyber Crime Verification", ScamInitials: "CC",
		SenderID: "TX-CBICRM", CallerNumber: "+91 11 4999 0021", CallerCaption: "Video call ended · 1 minute ago",
		Headline:     "A parcel in your name was seized",
		MessageBody:  "This is regarding FIR/2026/8841. A parcel containing contraband was intercepted in your name and your Aadhaar has been linked to a money laundering case. To verify your funds are clean and avoid arrest today, transfer ₹9,000 to the government verification account. It will be returned after clearance.",
		AccountCaptn: "Verification A/C", ScamMinor: 900000,
		WhyItWorks: "Fear plus an official-looking uniform on a video call. Victims are told to stay on the line and not consult family — isolation is the technique.",
		TheTruth:   "No police force in India arrests over UPI, and no investigation asks you to prove innocence by transferring money.",
	},
	{
		Key: "job", PersonaName: "Neha Verma",
		PersonaBlurb:    "You are twenty-three and looking for work. You pay your phone bill, your gym, and you have three job applications open on this phone right now.",
		MerchantAccount: "BANK_A-JUDGE-METRO-CARD", MerchantLabel: "Metro Card Top-up",
		MerchantSub: "Twice a week", MerchantInitial: "MC", EverydayMinor: 20000,
		ScamLabel: "TaskEarn HR Payout", ScamInitials: "TE",
		SenderID: "BZ-TSKERN", CallerNumber: "+91 63 1102 7745", CallerCaption: "Telegram message · 6 minutes ago",
		Headline:     "Unlock your ₹18,000 payout",
		MessageBody:  "Congratulations! You completed 40/40 review tasks. Your earnings of ₹18,000 are ready. As per company policy a refundable security deposit of ₹2,500 is required to release the payout to a new member account. Deposit now and receive ₹20,500 within 10 minutes.",
		AccountCaptn: "Deposit A/C", ScamMinor: 250000,
		WhyItWorks: "The first few small tasks really do pay, which buys the trust that the final, larger deposit spends.",
		TheTruth:   "A job that asks you to pay before it pays you is not a job. Real employers never take a deposit to release wages.",
	},
}

type judgeSessionResponse struct {
	SessionID       string `json:"session_id"`
	PayerAccount    string `json:"payer_account"`
	MerchantAccount string `json:"merchant_account"`
	MerchantLabel   string `json:"merchant_label"`
	ScamAccount     string `json:"scam_account"`
	ScamLabel       string `json:"scam_label"`
	// The full story for this run. The phone renders from this, so no scam copy is
	// hardcoded in the frontend and every scan can differ.
	Scenario judgeScenario `json:"scenario"`
	// Live progress, so the console can mirror the phone on the big screen without anyone
	// narrating it. Updated by POST /v1/judge/act.
	Act        string `json:"act"`
	ActLabel   string `json:"act_label"`
	UpdatedMs  int64  `json:"updated_ms"`
	LastRef    string `json:"last_ref,omitempty"`
	LastAction string `json:"last_action,omitempty"`
}

// pickScenario deals from a shuffled deck rather than sampling independently. Independent
// random choice can hand three judges in a row the same story purely by chance, which is the
// exact impression this rotation exists to avoid. Dealing guarantees all five appear before
// any repeats, and reshuffling never puts the last card of one deck first in the next.
//
// Guarded by judgeMu, which the caller holds.
var scenarioDeck []int

func pickScenario() judgeScenario {
	if len(judgeScenarios) == 1 {
		return judgeScenarios[0]
	}
	if len(scenarioDeck) == 0 {
		for i := range judgeScenarios {
			scenarioDeck = append(scenarioDeck, i)
		}
		rand.Shuffle(len(scenarioDeck), func(i, j int) {
			scenarioDeck[i], scenarioDeck[j] = scenarioDeck[j], scenarioDeck[i]
		})
		// Avoid the seam between decks repeating a story back to back.
		if judgeScenarios[scenarioDeck[0]].Key == lastScenarioKey {
			scenarioDeck[0], scenarioDeck[len(scenarioDeck)-1] = scenarioDeck[len(scenarioDeck)-1], scenarioDeck[0]
		}
	}
	sc := judgeScenarios[scenarioDeck[0]]
	scenarioDeck = scenarioDeck[1:]
	lastScenarioKey = sc.Key
	return sc
}

var lastScenarioKey string

func (s *Server) handleJudgeSession(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	run := time.Now().UnixNano()

	s.judgeMu.Lock()
	sc := pickScenario()
	s.judgeMu.Unlock()

	payer := fmt.Sprintf("BANK_A-JUDGE-%d", run)
	scam := fmt.Sprintf("BANK_A-JUDGE-SCAM-%d", run)

	// Seed real warm-up history for this payer through the real decision path — same pattern
	// as demo scenario A — so it has an account age, an hour histogram, and a velocity
	// baseline by the time the judge's own first tap happens. The amounts sit around this
	// persona's everyday spend so the history reads as theirs.
	base := sc.EverydayMinor
	for i := 0; i < 6; i++ {
		ev := &contract.Event{
			EndToEndID:            fmt.Sprintf("judge-%d-seed%d", run, i),
			Rail:                  contract.RailUPI,
			DebtorAccount:         payer,
			CreditorAccount:       sc.MerchantAccount,
			InstructedAmountMinor: base - base/4 + int64(i)*(base/12),
			Initiation:            "INTENT",
		}
		if _, _, err := s.DecideAndPersist(ctx, ev); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "seeding session: " + err.Error()})
			return
		}
	}

	resp := judgeSessionResponse{
		SessionID:       fmt.Sprintf("judge-%d", run),
		PayerAccount:    payer,
		MerchantAccount: sc.MerchantAccount,
		MerchantLabel:   sc.MerchantLabel,
		ScamAccount:     scam,
		ScamLabel:       sc.ScamLabel,
		Scenario:        sc,
		Act:             "intro",
		ActLabel:        "Phone connected — " + sc.PersonaName,
		UpdatedMs:       time.Now().UnixMilli(),
	}

	// Remember it so the presenter view on the big screen can follow this exact phone
	// without anyone typing an account number mid-demo.
	s.judgeMu.Lock()
	s.judgeSession = &resp
	s.judgeMu.Unlock()
	s.hub.Publish("judge_session", resp)

	writeJSON(w, http.StatusOK, resp)
}

// GET /v1/judge/session — whichever phone most recently scanned the QR. The console's
// presenter view polls this so it can highlight that payer's transactions as they land.
func (s *Server) handleJudgeSessionActive(w http.ResponseWriter, r *http.Request) {
	s.judgeMu.Lock()
	sess := s.judgeSession
	s.judgeMu.Unlock()
	if sess == nil {
		writeJSON(w, http.StatusOK, map[string]any{"session": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"session": sess})
}

var judgeActLabels = map[string]string{
	"intro":     "Reading the brief",
	"home":      "In the banking app",
	"chai_done": "Everyday payment cleared",
	"call":      "The scam message just arrived",
	"scam_pay":  "About to pay the fraudster",
	"warned":    "WARNED — reading our evidence",
	"cancelled": "Cancelled — money saved",
	"override":  "Overrode the warning — sent to analysts",
	"reveal":    "Looking at the proof",
}

// POST /v1/judge/act {"act":"warned","ref":"...","action":"STEP_UP_INTERSTITIAL"}
//
// The phone reports which beat it is on. This is presentation state only — it can never
// affect a decision — but it lets the big screen track the judge's story live instead of
// the presenter having to say "he's paying now". Rejected if it doesn't match the session
// currently on the QR, so a stale tab can't rewind the room's view.
func (s *Server) handleJudgeAct(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionID string `json:"session_id"`
		Act       string `json:"act"`
		Ref       string `json:"ref"`
		Action    string `json:"action"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	s.judgeMu.Lock()
	sess := s.judgeSession
	if sess == nil || (req.SessionID != "" && sess.SessionID != req.SessionID) {
		s.judgeMu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "reason": "not the active session"})
		return
	}
	updated := *sess
	updated.Act = req.Act
	updated.ActLabel = judgeActLabels[req.Act]
	if updated.ActLabel == "" {
		updated.ActLabel = req.Act
	}
	updated.UpdatedMs = time.Now().UnixMilli()
	if req.Ref != "" {
		updated.LastRef = req.Ref
	}
	if req.Action != "" {
		updated.LastAction = req.Action
	}
	s.judgeSession = &updated
	s.judgeMu.Unlock()

	s.hub.Publish("judge_session", updated)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
