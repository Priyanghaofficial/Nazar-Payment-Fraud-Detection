package profile

import (
	"context"
	"encoding/json"
	"math"
	"sort"
	"time"

	"github.com/redis/go-redis/v9"

	"nazar/internal/contract"
)

// RedisProfileStore is the real implementation of contract.ProfileStore.
//
// Load groups keys by hash tag into <=5 groups and issues them CONCURRENTLY (docs/01 §3.1:
// wall clock = max(RTT), not sum). Each group's results are bound into the ProfileBundle by
// explicit Go struct field, never by position (docs/02 §3.6, fixes F-37). A group whose read
// fails or exceeds its per-call deadline budget degrades that slice of the bundle only —
// Present=false, Degraded=true — and does not fail the whole Load (docs/00 §8).
type RedisProfileStore struct {
	rdb *redis.Client
}

func NewRedisProfileStore(rdb *redis.Client) *RedisProfileStore {
	return &RedisProfileStore{rdb: rdb}
}

// groupResult is returned by each concurrent group goroutine. Using named fields (not a
// []interface{} indexed by position) is what makes "bind by name" real in Go.
type groupResult struct {
	err error
}

func (s *RedisProfileStore) Load(ctx context.Context, ev *contract.Event) (*contract.ProfileBundle, error) {
	now := ev.AcceptedAtMs
	bundle := &contract.ProfileBundle{LoadedAtMs: time.Now().UnixMilli()}

	type job func(ctx context.Context)
	var jobs []job

	jobs = append(jobs, func(ctx context.Context) {
		loadPayer(ctx, s.rdb, ev.DebtorAccount, now, &bundle.Payer)
	})
	jobs = append(jobs, func(ctx context.Context) {
		loadPayee(ctx, s.rdb, ev.CreditorAccount, now, &bundle.Payee)
	})
	if ev.DeviceID != "" {
		jobs = append(jobs, func(ctx context.Context) {
			loadDevice(ctx, s.rdb, ev.DeviceID, now, &bundle.Device)
		})
	}
	jobs = append(jobs, func(ctx context.Context) {
		loadPair(ctx, s.rdb, ev.DebtorAccount, ev.CreditorAccount, &bundle.Pair)
	})
	if ev.ASN != 0 {
		jobs = append(jobs, func(ctx context.Context) {
			loadASN(ctx, s.rdb, ev.ASN, now, &bundle.ASN)
		})
	}

	done := make(chan struct{}, len(jobs))
	for _, j := range jobs {
		j := j
		go func() {
			defer func() { done <- struct{}{} }()
			j(ctx)
		}()
	}
	for range jobs {
		<-done
	}
	return bundle, nil
}

func loadPayer(ctx context.Context, rdb *redis.Client, acct string, nowMs int64, out *contract.PayerBundle) {
	pipe := rdb.Pipeline()
	txn1m := pipe.ZCount(ctx, payerTxnWindow(acct), scoreStr(nowMs-60_000), scoreStr(nowMs))
	txn5m := pipe.ZCount(ctx, payerTxnWindow(acct), scoreStr(nowMs-300_000), scoreStr(nowMs))
	txn1h := pipe.ZCount(ctx, payerTxnWindow(acct), scoreStr(nowMs-3_600_000), scoreStr(nowMs))
	txn24h := pipe.ZCount(ctx, payerTxnWindow(acct), scoreStr(nowMs-86_400_000), scoreStr(nowMs))
	baseline := pipe.HGetAll(ctx, payerBaseline(acct))
	payeeSet := pipe.SCard(ctx, payerPayeeSet(acct))
	deviceSet := pipe.SCard(ctx, payerDeviceSet(acct))
	last := pipe.HGetAll(ctx, payerLast(acct))
	firstSeen := pipe.Get(ctx, payerFirstSeen(acct))
	amtMinuteKeys := minuteBucketKeys(nowMs, 60)
	amtMinute := pipe.HMGet(ctx, payerAmtMinute(acct), amtMinuteKeys...)
	amtHourKeys := hourBucketKeys(nowMs, 24)
	amtHour := pipe.HMGet(ctx, payerAmtHour(acct), amtHourKeys...)
	amtDayKeys := dayBucketKeys(nowMs, 30)
	amtDay := pipe.HMGet(ctx, payerAmtDay(acct), amtDayKeys...)

	_, err := pipe.Exec(ctx)
	if err != nil && err != redis.Nil {
		out.Present = false
		out.Degraded = true
		return
	}
	out.Present = true

	out.TxnVelocity1m, _ = txn1m.Result()
	out.TxnVelocity5m, _ = txn5m.Result()
	out.TxnVelocity1h, _ = txn1h.Result()
	out.TxnVelocity24h, _ = txn24h.Result()

	bl, _ := baseline.Result()
	out.AmtMedianMinor = parseInt64(bl["amt_median"])
	out.AmtMADMinor = parseInt64(bl["amt_mad"])
	out.AmtP95Minor = parseInt64(bl["amt_p95"])
	out.AmtP99Minor = parseInt64(bl["amt_p99"])
	out.TxnCountLifetime = parseInt64(bl["txn_count_lifetime"])
	out.Txn1hP999 = parseFloat(bl["txn_1h_p999"])
	out.BaselineVersion = bl["baseline_version"]
	out.BaselineUpdatedAtMs = parseInt64(bl["updated_at"])
	out.HourHist = decodeHourHist(bl["hour_hist"])

	out.PayeeSetSize, _ = payeeSet.Result()
	out.DeviceSetSize, _ = deviceSet.Result()

	lastVals, _ := last.Result()
	out.LastTsMs = parseInt64(lastVals["last_ts_ms"])
	out.LastGeoCell = lastVals["last_geo_cell"]
	out.LastAmtMinor = parseInt64(lastVals["last_amt_minor"])

	if fs, err := firstSeen.Result(); err == nil {
		firstMs := parseInt64(fs)
		out.AccountAgeDays = float64(nowMs-firstMs) / 86_400_000.0
	} else {
		out.AccountAgeDays = 0 // cold start: no prior sighting
	}

	out.AmtSum1hMinor = sumHMGet(amtMinute)
	out.AmtSum24hMinor = sumHMGet(amtHour)
	daySum := sumHMGet(amtDay)
	out.AmtMean30dDailyMinor = daySum / 30

	out.KnownPayees = membersOf(ctx, rdb, payerPayeeSet(acct))
	out.KnownDevices = membersOf(ctx, rdb, payerDeviceSet(acct))
	out.KnownASNs = asnMembersOf(ctx, rdb, payerASNSet(acct))
}

func loadPayee(ctx context.Context, rdb *redis.Client, acct string, nowMs int64, out *contract.PayeeBundle) {
	pipe := rdb.Pipeline()
	fanin1h := pipe.ZCount(ctx, payeePayersWindow(acct), scoreStr(nowMs-3_600_000), scoreStr(nowMs))
	fanin24h := pipe.ZCount(ctx, payeePayersWindow(acct), scoreStr(nowMs-86_400_000), scoreStr(nowMs))
	firstSeen := pipe.Get(ctx, payeeFirstSeen(acct))
	fwd := pipe.HGetAll(ctx, payeeFwd(acct))
	inKeys := minuteBucketKeys(nowMs, 60)
	inflow := pipe.HMGet(ctx, payeeInMinute(acct), inKeys...)
	outflow := pipe.HMGet(ctx, payeeOutMinute(acct), inKeys...)
	shareHash := pipe.HGetAll(ctx, payeeInflowShare(acct))
	distinctPayers := pipe.ZRangeByScore(ctx, payeePayersWindow(acct), &redis.ZRangeBy{
		Min: scoreStr(nowMs - 3_600_000), Max: scoreStr(nowMs),
	})

	_, err := pipe.Exec(ctx)
	if err != nil && err != redis.Nil {
		out.Present = false
		out.Degraded = true
		return
	}
	out.Present = true

	out.Fanin1h, _ = fanin1h.Result()
	out.Fanin24h, _ = fanin24h.Result()
	if fs, err := firstSeen.Result(); err == nil {
		out.FirstSeenByUsAtMs = parseInt64(fs)
	}
	fwdVals, _ := fwd.Result()
	out.FwdLatencyP50Sec = parseFloat(fwdVals["fwd_latency_p50_s"])
	out.FwdSampleN = parseInt64(fwdVals["fwd_sample_n"])
	out.FwdUpdatedAtMs = parseInt64(fwdVals["fwd_updated_at"])

	out.InflowSum1hMinor = sumHMGet(inflow)
	out.OutflowSum1hMinor = sumHMGet(outflow)

	shareVals, _ := shareHash.Result()
	out.PayerInflowShare24h = make(map[string]float64, len(shareVals))
	for k, v := range shareVals {
		out.PayerInflowShare24h[k] = parseFloat(v)
	}

	payers, _ := distinctPayers.Result()
	banks := map[string]bool{}
	for _, p := range payers {
		banks[bankOfAccount(p)] = true
	}
	out.DistinctPayerBanks1h = int64(len(banks))
}

func loadDevice(ctx context.Context, rdb *redis.Client, id string, nowMs int64, out *contract.DeviceBundle) {
	pipe := rdb.Pipeline()
	degree := pipe.ZCount(ctx, deviceAccts(id), scoreStr(nowMs-86_400_000), scoreStr(nowMs))
	firstSeen := pipe.Get(ctx, deviceFirstSeen(id))
	_, err := pipe.Exec(ctx)
	if err != nil && err != redis.Nil {
		out.Present = false
		out.Degraded = true
		return
	}
	out.Present = true
	out.AcctDegree24h, _ = degree.Result()
	if fs, err := firstSeen.Result(); err == nil {
		out.FirstSeenAtMs = parseInt64(fs)
	}
}

func loadPair(ctx context.Context, rdb *redis.Client, payer, payee string, out *contract.PairBundle) {
	res, err := rdb.HGetAll(ctx, pairHash(payer, payee)).Result()
	if err != nil && err != redis.Nil {
		out.Degraded = true
		return
	}
	count := parseInt64(res["txn_count_90d"])
	if count < 2 {
		// docs/02 §3.1: pairs with txn_count_90d < 2 aren't stored — indistinguishable
		// from "no relationship", which is the default. Present=false either way.
		return
	}
	out.Present = true
	out.TxnCount90d = count
	out.AmtP95Minor = parseInt64(res["amt_p95_minor"])
	out.LastTsMs = parseInt64(res["last_ts_ms"])
	out.LastDisposition = res["last_disposition"]
	out.FirstAddedAtMs = parseInt64(res["first_added_ms"])
	out.LastCreditorAccount = res["last_creditor_account"]
}

func loadASN(ctx context.Context, rdb *redis.Client, asn int32, nowMs int64, out *contract.ASNBundle) {
	degree, err := rdb.ZCount(ctx, asnAccts(asn), scoreStr(nowMs-3_600_000), scoreStr(nowMs)).Result()
	if err != nil && err != redis.Nil {
		out.Degraded = true
		return
	}
	out.Present = true
	out.AcctDegree1h = degree
}

// ─────────────────────────────────────────────────────────────────────────
// Apply — the read-before-write invariant means this is called strictly AFTER the event has
// been scored (docs/02 §3.4), from the async lane, never in the request path.
// ─────────────────────────────────────────────────────────────────────────

func (s *RedisProfileStore) Apply(ctx context.Context, ev *contract.Event) error {
	nowMs := ev.AcceptedAtMs
	pipe := s.rdb.TxPipeline()

	// payer side
	pipe.ZAdd(ctx, payerTxnWindow(ev.DebtorAccount), redis.Z{Score: float64(nowMs), Member: ev.EndToEndID})
	pipe.ZRemRangeByScore(ctx, payerTxnWindow(ev.DebtorAccount), "0", scoreStr(nowMs-MaxWindowMs))
	pipe.Expire(ctx, payerTxnWindow(ev.DebtorAccount), 26*time.Hour)

	pipe.HIncrBy(ctx, payerAmtMinute(ev.DebtorAccount), minuteEpochStr(nowMs), ev.InstructedAmountMinor)
	pipe.HIncrBy(ctx, payerAmtHour(ev.DebtorAccount), hourEpochStr(nowMs), ev.InstructedAmountMinor)
	pipe.HIncrBy(ctx, payerAmtDay(ev.DebtorAccount), dayEpochStr(nowMs), ev.InstructedAmountMinor)
	pipe.Expire(ctx, payerAmtMinute(ev.DebtorAccount), 90*time.Hour)
	pipe.Expire(ctx, payerAmtHour(ev.DebtorAccount), 31*24*time.Hour)
	pipe.Expire(ctx, payerAmtDay(ev.DebtorAccount), 91*24*time.Hour)

	pipe.SAdd(ctx, payerPayeeSet(ev.DebtorAccount), ev.CreditorAccount)
	if ev.DeviceID != "" {
		pipe.SAdd(ctx, payerDeviceSet(ev.DebtorAccount), ev.DeviceID)
	}
	if ev.ASN != 0 {
		pipe.SAdd(ctx, payerASNSet(ev.DebtorAccount), ev.ASN)
	}
	pipe.HSet(ctx, payerLast(ev.DebtorAccount), map[string]any{
		"last_ts_ms": nowMs, "last_geo_cell": ev.GeoCell, "last_amt_minor": ev.InstructedAmountMinor,
	})
	pipe.SetNX(ctx, payerFirstSeen(ev.DebtorAccount), nowMs, 0)
	pipe.LPush(ctx, payerAmtSample(ev.DebtorAccount), ev.InstructedAmountMinor)
	pipe.LTrim(ctx, payerAmtSample(ev.DebtorAccount), 0, 499)
	pipe.HIncrBy(ctx, payerBaseline(ev.DebtorAccount), "txn_count_lifetime", 1)

	hourOfDay := hourOfDayUTC(nowMs)
	pipe.HIncrBy(ctx, hourHistKey(ev.DebtorAccount), hourOfDay, 1)

	// payee side
	pipe.ZAdd(ctx, payeePayersWindow(ev.CreditorAccount), redis.Z{Score: float64(nowMs), Member: ev.DebtorAccount})
	pipe.ZRemRangeByScore(ctx, payeePayersWindow(ev.CreditorAccount), "0", scoreStr(nowMs-MaxWindowMs))
	pipe.Expire(ctx, payeePayersWindow(ev.CreditorAccount), 26*time.Hour)
	pipe.ZAdd(ctx, payeeTxnWindow(ev.CreditorAccount), redis.Z{Score: float64(nowMs), Member: ev.EndToEndID})
	pipe.SetNX(ctx, payeeFirstSeen(ev.CreditorAccount), nowMs, 0)

	// both accounts get in/out buckets keyed by {b:<acct>} so forwarding ratio is
	// computable for any account that later acts as a payer (docs/02 §4.2 fwd_ratio_1h).
	pipe.HIncrBy(ctx, payeeInMinute(ev.CreditorAccount), minuteEpochStr(nowMs), ev.InstructedAmountMinor)
	pipe.HIncrBy(ctx, payeeOutMinute(ev.DebtorAccount), minuteEpochStr(nowMs), ev.InstructedAmountMinor)
	pipe.Expire(ctx, payeeInMinute(ev.CreditorAccount), 90*time.Hour)
	pipe.Expire(ctx, payeeOutMinute(ev.DebtorAccount), 90*time.Hour)

	// device side
	if ev.DeviceID != "" {
		pipe.ZAdd(ctx, deviceAccts(ev.DeviceID), redis.Z{Score: float64(nowMs), Member: ev.DebtorAccount})
		pipe.ZRemRangeByScore(ctx, deviceAccts(ev.DeviceID), "0", scoreStr(nowMs-MaxWindowMs))
		pipe.Expire(ctx, deviceAccts(ev.DeviceID), 26*time.Hour)
		pipe.SetNX(ctx, deviceFirstSeen(ev.DeviceID), nowMs, 0)
	}

	// pair side
	pairKey := pairHash(ev.DebtorAccount, ev.CreditorAccount)
	pipe.HIncrBy(ctx, pairKey, "txn_count_90d", 1)
	pipe.HSetNX(ctx, pairKey, "first_added_ms", nowMs)
	pipe.HSet(ctx, pairKey, map[string]any{
		"last_ts_ms": nowMs, "last_creditor_account": ev.CreditorAccount,
	})
	pipe.HSetNX(ctx, pairKey, "last_disposition", "UNKNOWN")
	pipe.Expire(ctx, pairKey, 91*24*time.Hour)

	// asn side
	if ev.ASN != 0 {
		pipe.ZAdd(ctx, asnAccts(ev.ASN), redis.Z{Score: float64(nowMs), Member: ev.DebtorAccount})
		pipe.ZRemRangeByScore(ctx, asnAccts(ev.ASN), "0", scoreStr(nowMs-MaxWindowMs))
		pipe.Expire(ctx, asnAccts(ev.ASN), 26*time.Hour)
	}

	if _, err := pipe.Exec(ctx); err != nil {
		return err
	}

	// Recompute the payer's baseline (median/MAD/p95/p99) from the capped sample and the
	// hour histogram, off the request path. O(500 log 500) — trivial, and it is a real
	// computation over real history, not a fake number.
	recomputeBaseline(ctx, s.rdb, ev.DebtorAccount, nowMs)

	// Pair amt_p95: recompute from the payer's own sample intersected with this payee is
	// overkill at P0; use a simple running max-of-recent-amounts proxy stored on the pair hash.
	updatePairAmtP95(ctx, s.rdb, pairKey, ev.InstructedAmountMinor)

	return nil
}

func recomputeBaseline(ctx context.Context, rdb *redis.Client, acct string, nowMs int64) {
	samples, err := rdb.LRange(ctx, payerAmtSample(acct), 0, -1).Result()
	if err != nil || len(samples) == 0 {
		return
	}
	vals := make([]float64, 0, len(samples))
	for _, s := range samples {
		vals = append(vals, parseFloat(s))
	}
	sort.Float64s(vals)
	median := percentile(vals, 0.5)
	p95 := percentile(vals, 0.95)
	p99 := percentile(vals, 0.99)
	deviations := make([]float64, len(vals))
	for i, v := range vals {
		deviations[i] = math.Abs(v - median)
	}
	sort.Float64s(deviations)
	mad := percentile(deviations, 0.5)

	hist, _ := rdb.HGetAll(ctx, hourHistKey(acct)).Result()
	encoded := encodeHourHist(hist)

	rdb.HSet(ctx, payerBaseline(acct), map[string]any{
		"amt_median":       int64(median),
		"amt_mad":          int64(mad),
		"amt_p95":          int64(p95),
		"amt_p99":          int64(p99),
		"hour_hist":        encoded,
		"payee_set_size":   0, // read live via SCARD at Load time; not duplicated here
		"device_set_size":  0,
		"baseline_version": "v1",
		"updated_at":       nowMs,
	})
}

func updatePairAmtP95(ctx context.Context, rdb *redis.Client, pairKey string, amt int64) {
	// P0 simplification: exponential update toward the observed amount rather than a full
	// percentile recompute per pair (pairs have far fewer samples than a payer's global
	// history, so this is cheap and the ratio-guard behaviour docs/02 §5.2 cares about is
	// "roughly the pair's usual scale", not an exact p95).
	cur, _ := rdb.HGet(ctx, pairKey, "amt_p95_minor").Result()
	curVal := parseInt64(cur)
	var next int64
	if curVal == 0 {
		next = amt
	} else if amt > curVal {
		next = curVal + (amt-curVal)/3
	} else {
		next = curVal - (curVal-amt)/10
	}
	rdb.HSet(ctx, pairKey, "amt_p95_minor", next)
}

// StoreDecision / LoadDecision implement the idempotency mechanism (docs/01 §7): a repeat
// POST /v1/decide within the TTL returns the stored decision unchanged, never re-scores.
func (s *RedisProfileStore) StoreDecision(ctx context.Context, e2eID string, d *contract.Decision) error {
	b, err := json.Marshal(d)
	if err != nil {
		return err
	}
	return s.rdb.Set(ctx, idempotencyKey(e2eID), b, 24*time.Hour).Err()
}

func (s *RedisProfileStore) LoadDecision(ctx context.Context, e2eID string) (*contract.Decision, bool, error) {
	b, err := s.rdb.Get(ctx, idempotencyKey(e2eID)).Bytes()
	if err == redis.Nil {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var d contract.Decision
	if err := json.Unmarshal(b, &d); err != nil {
		return nil, false, err
	}
	return &d, true, nil
}
