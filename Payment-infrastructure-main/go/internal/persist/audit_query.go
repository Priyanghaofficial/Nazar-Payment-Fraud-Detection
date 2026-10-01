package persist

import (
	"context"
	"database/sql"
	"fmt"

	"nazar/internal/audit"
)

// GetChainTip returns the last chain_seq/hash written on a shard, so a restarting process
// can resume its in-memory audit.Chain instead of starting a second genesis on top of the
// same shard (see audit.Chain.Resume). Zero values mean the chain is genuinely empty.
func GetChainTip(ctx context.Context, db *sql.DB, shard int16) (seq int64, hash []byte, err error) {
	row := db.QueryRowContext(ctx, `
		SELECT chain_seq, hash FROM decisions
		WHERE decision_shard = $1
		ORDER BY chain_seq DESC LIMIT 1`, shard)
	err = row.Scan(&seq, &hash)
	if err == sql.ErrNoRows {
		return 0, nil, nil
	}
	if err != nil {
		return 0, nil, fmt.Errorf("persist: chain tip: %w", err)
	}
	return seq, hash, nil
}

// LoadChainForVerification reads every LIVE decision on a shard, ordered by chain_seq, and
// returns exactly the fields audit.Verify needs to recompute the chain independently of
// whatever the row itself claims — this is what makes "Verify Chain" in the console a real
// check rather than reading a stored boolean.
func LoadChainForVerification(ctx context.Context, db *sql.DB, shard int16) ([]audit.CanonicalRecord, [][]byte, [][]byte, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT end_to_end_id, decision_seq, extract(epoch from accepted_at)*1000, extract(epoch from decided_at)*1000,
		       action, p_model, policy_version, model_bundle_version, rules_version, prev_hash, hash
		FROM decisions
		WHERE decision_shard = $1
		ORDER BY chain_seq ASC`, shard)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("persist: query chain: %w", err)
	}
	defer rows.Close()

	var recs []audit.CanonicalRecord
	var prevHashes, hashes [][]byte
	for rows.Next() {
		var rec audit.CanonicalRecord
		var acceptedMs, decidedMs float64
		var pModel sql.NullFloat64
		var prevHash, hash []byte
		if err := rows.Scan(&rec.EndToEndID, &rec.DecisionSeq, &acceptedMs, &decidedMs,
			&rec.Action, &pModel, &rec.PolicyVersion, &rec.ModelBundleVersion, &rec.RulesVersion,
			&prevHash, &hash); err != nil {
			return nil, nil, nil, err
		}
		rec.AcceptedAtMs = int64(acceptedMs)
		rec.DecidedAtMs = int64(decidedMs)
		if pModel.Valid {
			rec.PModel = fmt.Sprintf("%.6f", pModel.Float64)
		} else {
			rec.PModel = "nil"
		}
		recs = append(recs, rec)
		prevHashes = append(prevHashes, prevHash)
		hashes = append(hashes, hash)
	}
	return recs, prevHashes, hashes, rows.Err()
}
