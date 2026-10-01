// Package consortium implements the P0 tokenised-set-membership consortium (docs/05 §4).
//
// P0 simplification, stated per CLAUDE.md's own table: "HMAC + epoch field + honest
// framing", NOT the OPRF option (docs/05 §4.1 Option B). This means: the registry operator
// cannot invert a token back to an account number, but any MEMBER who holds the shared
// pepper for an epoch CAN — because they can brute-force low-entropy Indian account numbers
// against the HMAC. That is a real, named limitation, not hidden behind the word
// "non-invertible" (docs/05 §4.1's whole point). Say "pseudonym, not confidentiality
// control" — never "non-invertible" — when describing this to anyone.
package consortium

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// Tokeniser derives the pseudonymous token for an identifier under a given pepper epoch.
type Tokeniser struct {
	// pepperByEpoch simulates key rotation (docs/05 §4.1's "ep" field): a real deployment
	// distributes a new pepper to all members out of band each epoch and retires the old
	// one after a grace window. Here it's an in-memory map for the demo.
	pepperByEpoch map[int][]byte
	currentEpoch  int
}

func NewTokeniser(initialPepper []byte) *Tokeniser {
	return &Tokeniser{pepperByEpoch: map[int][]byte{1: initialPepper}, currentEpoch: 1}
}

func (t *Tokeniser) CurrentEpoch() int { return t.currentEpoch }

// RotateEpoch introduces a new pepper and advances the current epoch — the mechanism that
// makes a pepper compromise recoverable (docs/05 §4.1: "no rotation path" was the finding
// against the previous design). Old epochs are kept so previously issued tokens still verify.
func (t *Tokeniser) RotateEpoch(newPepper []byte) int {
	t.currentEpoch++
	t.pepperByEpoch[t.currentEpoch] = newPepper
	return t.currentEpoch
}

// Token computes HMAC-SHA256(pepper[epoch], identifier), hex-encoded. kind disambiguates
// the identifier namespace (e.g. "creditor_account") so the same raw string under different
// kinds never collides.
func (t *Tokeniser) Token(epoch int, kind, identifier string) (string, error) {
	pepper, ok := t.pepperByEpoch[epoch]
	if !ok {
		return "", fmt.Errorf("consortium: unknown pepper epoch %d", epoch)
	}
	mac := hmac.New(sha256.New, pepper)
	mac.Write([]byte(kind + ":" + identifier))
	return hex.EncodeToString(mac.Sum(nil)), nil
}

// TokenCurrentEpoch is the common case: token an identifier under whatever epoch is active now.
func (t *Tokeniser) TokenCurrentEpoch(kind, identifier string) (string, error) {
	return t.Token(t.currentEpoch, kind, identifier)
}
