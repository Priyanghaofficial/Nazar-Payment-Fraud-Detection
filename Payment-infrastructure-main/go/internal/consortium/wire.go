package consortium

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

// WireEntry is the literal wire format from docs/05 §4.6. What does NOT cross: account
// numbers, VPAs, names, phone numbers, amounts, device IDs, customer activity timestamps —
// only the token and metadata about the report itself.
type WireEntry struct {
	V           int    `json:"v"`
	Op          string `json:"op"`
	Epoch       int    `json:"ep"`
	Token       string `json:"token"`
	Kind        string `json:"kind"`
	ThreatClass string `json:"threat_class"`
	Reporter    string `json:"reporter"`
	LegalEntity string `json:"legal_entity"`
	ReportedAt  string `json:"reported_at"`
	ExpiresAt   string `json:"expires_at"`
	ChainSeq    int64  `json:"chain_seq"`
	PrevHash    string `json:"prev_hash"`
	Hash        string `json:"hash"`
	Sig         string `json:"sig"`
}

// signingKeys is a P0 stand-in for real per-reporter asymmetric keys (docs/05 §4.6's `sig`
// field implies ECDSA/Ed25519 in a real deployment — verifiable by any third party without
// a shared secret). Here it's HMAC with a key "held" by each reporter, which proves the
// MECHANISM (every entry is signed, so a reporter's later-dismissed flags are provably
// theirs — docs/05 §4.3) without building a PKI for a hackathon demo. Named honestly: this
// is a shared-secret MAC, not a public-key signature, and swapping it for real Ed25519 is a
// Type 2 change behind the same `Sig` field.
var signingKeys = map[string][]byte{
	"BANK_A": []byte("demo-signing-key-bank-a-not-for-production"),
	"BANK_B": []byte("demo-signing-key-bank-b-not-for-production"),
}

func sign(reporter string, payload []byte) string {
	key, ok := signingKeys[reporter]
	if !ok {
		key = []byte("demo-default-key")
	}
	mac := hmac.New(sha256.New, key)
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

// Wire renders an Entry as the literal bytes docs/05 §4.6 specifies — this is what
// GET /v1/federation/wire/{id} returns.
func Wire(e *Entry, op Op) WireEntry {
	w := WireEntry{
		V: 2, Op: string(op), Epoch: e.Epoch, Token: e.Token, Kind: e.Kind,
		ThreatClass: e.ThreatClass, Reporter: e.Reporter, LegalEntity: e.LegalEntity,
		ReportedAt: e.CreatedAt.Format("2006-01-02T15:04:05Z"),
		ExpiresAt:  e.ExpiresAt.Format("2006-01-02T15:04:05Z"),
		ChainSeq:   e.ChainSeq, PrevHash: hexOrEmpty(e.PrevHash), Hash: hexOrEmpty(e.Hash),
	}
	w.Sig = sign(e.Reporter, []byte(w.Token+w.ThreatClass+w.ReportedAt+w.Hash))
	return w
}

func hexOrEmpty(b []byte) string {
	if b == nil {
		return ""
	}
	return hex.EncodeToString(b)
}
