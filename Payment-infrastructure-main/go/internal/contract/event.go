// Package contract holds the Type 1 wire and record contracts: the canonical event, the
// profile bundle, findings, feature vectors, and the decision record. See docs/02 §1 and
// CLAUDE.md ("proto/ changes require an ADR"). Nothing in this package performs I/O.
package contract

// Rail enumerates the payment rails Nazar scores. Card is split CNP/CP because
// LossGivenFraud (docs/04 §2.1) distinguishes them.
type Rail string

const (
	RailUPI      Rail = "UPI"
	RailIMPS     Rail = "IMPS"
	RailNEFT     Rail = "NEFT"
	RailCardCNP  Rail = "CARD_CNP"
	RailCardCP   Rail = "CARD_CP"
)

// ClaimedFacts are asserted by the caller and are NEVER trusted as feature inputs.
// They are quarantined here (docs/02 §1, fixes F-43) and retained only because a false
// claim is itself evidence. Server-derived values always win.
type ClaimedFacts struct {
	CreditorAccountOpenedMs int64 `json:"creditor_account_opened_ms,omitempty"`
	DeviceFirstSeenMs       int64 `json:"device_first_seen_ms,omitempty"`
}

// Event is the canonical transaction event. Additive-only evolution: new fields get new
// JSON keys; readers (Go's encoding/json into this struct) ignore unknown fields for free.
type Event struct {
	// identity
	EndToEndID    string `json:"end_to_end_id"`
	EventTsMs     int64  `json:"event_ts_ms"`     // producer-claimed. NEVER used for windowing.
	AcceptedAtMs  int64  `json:"accepted_at_ms"`  // stamped by Nazar at the trust boundary.
	Rail          Rail   `json:"rail"`
	Channel       string `json:"channel"`
	BankInstance  string `json:"bank_instance"`

	// parties
	DebtorAccount   string `json:"debtor_account"`
	DebtorVPA       string `json:"debtor_vpa,omitempty"`
	CreditorAccount string `json:"creditor_account"`
	CreditorVPA     string `json:"creditor_vpa,omitempty"`
	CreditorIFSC    string `json:"creditor_ifsc,omitempty"`

	// money — integer minor units, always
	InstructedAmountMinor int64  `json:"instructed_amount_minor"`
	Currency              string `json:"currency"`

	// channel context
	DeviceID   string `json:"device_id,omitempty"`
	IP         string `json:"ip,omitempty"`
	ASN        int32  `json:"asn,omitempty"`
	GeoCell    string `json:"geo_cell,omitempty"`
	SessionID  string `json:"session_id,omitempty"`
	AppVersion string `json:"app_version,omitempty"`

	// payment context
	Initiation     string `json:"initiation,omitempty"`
	RemittanceInfo string `json:"remittance_info,omitempty"` // ATTACKER-CONTROLLED. Never to an LLM.

	Claimed ClaimedFacts `json:"claimed,omitempty"`

	SchemaVersion uint32 `json:"schema_version"`
}
