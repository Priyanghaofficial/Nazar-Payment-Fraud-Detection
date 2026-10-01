// Package features computes the feature vector as PURE functions of (ProfileBundle, Event).
// Structural rule (docs/00 §9, lint-enforced by go/test/invariants):
// internal/features may NOT import internal/profile or any Redis/Postgres client. Every
// function here takes already-loaded state and returns values; none of them touch the
// network. This is what makes golden_features testable from checked-in fixtures.
package features

import (
	"math"
	"strconv"
	"strings"

	"nazar/internal/contract"
)

// Compute assembles the full feature vector for one scoring pass. nowMs is the event's own
// accepted_at_ms — never wall-clock time.Now() — so the computation is deterministic and
// replayable (D1/D2).
func Compute(ev *contract.Event, pb *contract.ProfileBundle, nowMs int64) *contract.FeatureVector {
	fv := contract.NewFeatureVector()

	computePayerFeatures(fv, ev, pb, nowMs)
	computeCounterpartyFeatures(fv, ev, pb, nowMs)
	computeChannelFeatures(fv, ev, pb, nowMs)

	// cold_start_features_n: computed last, over everything computed so far.
	n := 0
	for _, st := range fv.Status {
		if st == contract.StatusNotEvaluated || st == contract.StatusNotApplicable {
			n++
		}
	}
	fv.Set("cold_start_features_n", float64(n))

	return fv
}

func computePayerFeatures(fv *contract.FeatureVector, ev *contract.Event, pb *contract.ProfileBundle, nowMs int64) {
	p := pb.Payer
	if !p.Present || p.Degraded {
		for _, id := range []string{"amt_robust_z", "amt_over_p95", "hour_surprisal", "txn_velocity_1m",
			"txn_velocity_5m", "txn_velocity_1h", "txn_velocity_24h", "amt_velocity_1h", "amt_velocity_24h",
			"account_age_days", "dormancy_days", "baseline_staleness_h"} {
			fv.NotEvaluated(id, "PROFILE_DEGRADED")
		}
		return
	}

	amt := float64(ev.InstructedAmountMinor)

	// amt_robust_z — docs/02 §5.1
	if p.TxnCountLifetime < 10 {
		fv.NotEvaluated("amt_robust_z", "COLD_START")
	} else {
		median := float64(p.AmtMedianMinor)
		mad := float64(p.AmtMADMinor)
		madEff := math.Max(mad, math.Max(0.02*median, 100))
		z := 0.6745 * (amt - median) / madEff
		if math.IsInf(z, 0) || math.IsNaN(z) || math.Abs(z) > 25 {
			fv.NotEvaluated("amt_robust_z", "OFF_SCALE")
		} else {
			fv.Set("amt_robust_z", z)
		}
	}

	// amt_over_p95 — docs/02 §5.2
	if p.AmtP95Minor <= 0 {
		fv.NotEvaluated("amt_over_p95", "COLD_START")
	} else {
		ratio := amt / float64(p.AmtP95Minor)
		fv.Set("amt_over_p95", math.Min(ratio, 20))
	}

	// hour_surprisal
	hour := hourOfDay(nowMs)
	prob := p.HourHist[hour]
	if prob <= 0 {
		prob = 1e-6
	}
	fv.Set("hour_surprisal", -math.Log2(prob))

	fv.Set("txn_velocity_1m", float64(p.TxnVelocity1m))
	fv.Set("txn_velocity_5m", float64(p.TxnVelocity5m))
	fv.Set("txn_velocity_1h", float64(p.TxnVelocity1h))
	fv.Set("txn_velocity_24h", float64(p.TxnVelocity24h))

	if p.AmtMean30dDailyMinor <= 0 {
		fv.NotEvaluated("amt_velocity_1h", "COLD_START")
		fv.NotEvaluated("amt_velocity_24h", "COLD_START")
	} else {
		fv.Set("amt_velocity_1h", float64(p.AmtSum1hMinor)/float64(p.AmtMean30dDailyMinor))
		fv.Set("amt_velocity_24h", float64(p.AmtSum24hMinor)/float64(p.AmtMean30dDailyMinor))
	}

	fv.Set("account_age_days", p.AccountAgeDays)

	if p.LastTsMs == 0 {
		fv.NotApplicable("dormancy_days", "NO_PRIOR_TXN")
	} else {
		fv.Set("dormancy_days", float64(nowMs-p.LastTsMs)/86_400_000.0)
	}

	if p.BaselineUpdatedAtMs == 0 {
		fv.NotEvaluated("baseline_staleness_h", "COLD_START")
	} else {
		fv.Set("baseline_staleness_h", float64(nowMs-p.BaselineUpdatedAtMs)/3_600_000.0)
	}
}

func computeCounterpartyFeatures(fv *contract.FeatureVector, ev *contract.Event, pb *contract.ProfileBundle, nowMs int64) {
	payee := pb.Payee
	pair := pb.Pair
	payer := pb.Payer

	if !payee.Present || payee.Degraded {
		for _, id := range []string{"payee_is_new_to_payer", "payee_first_seen_by_us_days", "payee_fanin_1h",
			"payee_fanin_24h", "payee_fanin_burstiness", "payee_fwd_latency_p50_s", "payee_fwd_ratio_1h",
			"payee_inflow_concentration", "payee_distinct_payer_banks_1h"} {
			fv.NotEvaluated(id, "PROFILE_DEGRADED")
		}
	} else {
		isNew := payer.Present && !payer.KnownPayees[ev.CreditorAccount]
		fv.Set("payee_is_new_to_payer", boolF(isNew))

		if payee.FirstSeenByUsAtMs == 0 {
			fv.NotEvaluated("payee_first_seen_by_us_days", "COLD_START")
		} else {
			fv.Set("payee_first_seen_by_us_days", float64(nowMs-payee.FirstSeenByUsAtMs)/86_400_000.0)
		}

		fv.Set("payee_fanin_1h", float64(payee.Fanin1h))
		fv.Set("payee_fanin_24h", float64(payee.Fanin24h))

		// payee_fanin_burstiness — docs/02 §5.3: requires fanin_24h >= 6
		if payee.Fanin24h < 6 {
			fv.NotApplicable("payee_fanin_burstiness", "FANIN_24H_BELOW_GATE")
		} else {
			hourlyAvg := float64(payee.Fanin24h) / 24.0
			fv.Set("payee_fanin_burstiness", float64(payee.Fanin1h)/hourlyAvg)
		}

		// forwarding features — docs/02 §5.4
		stale := payee.FwdUpdatedAtMs != 0 && (nowMs-payee.FwdUpdatedAtMs) > 15*60*1000
		if payee.FwdSampleN < 3 {
			fv.NotApplicable("payee_fwd_latency_p50_s", "INSUFFICIENT_SAMPLE")
		} else if stale {
			fv.NotEvaluated("payee_fwd_latency_p50_s", "STALE")
		} else {
			fv.Set("payee_fwd_latency_p50_s", payee.FwdLatencyP50Sec)
		}

		if payee.InflowSum1hMinor < 100 || payee.FwdSampleN < 3 {
			fv.NotApplicable("payee_fwd_ratio_1h", "INSUFFICIENT_INFLOW_OR_SAMPLE")
		} else if stale {
			fv.NotEvaluated("payee_fwd_ratio_1h", "STALE")
		} else {
			fv.Set("payee_fwd_ratio_1h", float64(payee.OutflowSum1hMinor)/float64(payee.InflowSum1hMinor))
		}

		// payee_inflow_concentration — HHI over payer share of 24h inflow
		if len(payee.PayerInflowShare24h) == 0 {
			fv.NotEvaluated("payee_inflow_concentration", "COLD_START")
		} else {
			var hhi float64
			for _, share := range payee.PayerInflowShare24h {
				hhi += share * share
			}
			fv.Set("payee_inflow_concentration", hhi)
		}

		fv.Set("payee_distinct_payer_banks_1h", float64(payee.DistinctPayerBanks1h))
	}

	if !pair.Present {
		fv.NotApplicable("pair_txn_count_90d", "NO_RELATIONSHIP")
		fv.NotApplicable("pair_amt_ratio_p95", "NO_RELATIONSHIP")
	} else {
		fv.Set("pair_txn_count_90d", float64(pair.TxnCount90d))
		if pair.AmtP95Minor <= 0 || pair.TxnCount90d < 3 {
			fv.NotApplicable("pair_amt_ratio_p95", "INSUFFICIENT_PAIR_HISTORY")
		} else {
			fv.Set("pair_amt_ratio_p95", float64(ev.InstructedAmountMinor)/float64(pair.AmtP95Minor))
		}
	}
}

func computeChannelFeatures(fv *contract.FeatureVector, ev *contract.Event, pb *contract.ProfileBundle, nowMs int64) {
	payer := pb.Payer
	device := pb.Device
	asn := pb.ASN

	if ev.DeviceID == "" {
		fv.NotApplicable("device_is_new_to_payer", "NO_DEVICE_ID")
		fv.NotApplicable("device_first_seen_hours", "NO_DEVICE_ID")
		fv.NotApplicable("device_acct_degree_24h", "NO_DEVICE_ID")
	} else if !device.Present || device.Degraded {
		fv.NotEvaluated("device_is_new_to_payer", "PROFILE_DEGRADED")
		fv.NotEvaluated("device_first_seen_hours", "PROFILE_DEGRADED")
		fv.NotEvaluated("device_acct_degree_24h", "PROFILE_DEGRADED")
	} else {
		isNew := payer.Present && !payer.KnownDevices[ev.DeviceID]
		fv.Set("device_is_new_to_payer", boolF(isNew))
		if device.FirstSeenAtMs == 0 {
			fv.NotEvaluated("device_first_seen_hours", "COLD_START")
		} else {
			fv.Set("device_first_seen_hours", float64(nowMs-device.FirstSeenAtMs)/3_600_000.0)
		}
		fv.Set("device_acct_degree_24h", float64(device.AcctDegree24h))
	}

	if ev.ASN == 0 {
		fv.NotApplicable("asn_is_new_to_payer", "NO_ASN")
		fv.NotApplicable("asn_acct_degree_1h", "NO_ASN")
	} else if !asn.Present || asn.Degraded {
		fv.NotEvaluated("asn_is_new_to_payer", "PROFILE_DEGRADED")
		fv.NotEvaluated("asn_acct_degree_1h", "PROFILE_DEGRADED")
	} else {
		isNew := payer.Present && !payer.KnownASNs[ev.ASN]
		fv.Set("asn_is_new_to_payer", boolF(isNew))
		fv.Set("asn_acct_degree_1h", float64(asn.AcctDegree1h))
	}

	// geo_jump_kmh — docs/02 §5.5
	lat1, lon1, ok1 := parseGeoCell(payer.LastGeoCell)
	lat2, lon2, ok2 := parseGeoCell(ev.GeoCell)
	if !payer.Present || !ok1 || !ok2 || payer.LastGeoCell == ev.GeoCell {
		fv.NotApplicable("geo_jump_kmh", "GEO_UNAVAILABLE_OR_UNCHANGED")
	} else {
		distKm := haversineKm(lat1, lon1, lat2, lon2)
		dtSec := math.Max(float64(nowMs-payer.LastTsMs)/1000.0, 60)
		fv.Set("geo_jump_kmh", distKm/(dtSec/3600.0))
	}
}

func boolF(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

func hourOfDay(ms int64) int {
	// UTC hour-of-day, matching profile.hourOfDayUTC's bucketing convention.
	secOfDay := (ms / 1000) % 86400
	if secOfDay < 0 {
		secOfDay += 86400
	}
	return int(secOfDay / 3600)
}

// parseGeoCell expects "lat:lon" (P0 convention — see py/generator). A real deployment
// would resolve a proper geo-cell/geohash; this is a Type 2 choice behind this one function.
func parseGeoCell(cell string) (lat, lon float64, ok bool) {
	parts := strings.Split(cell, ":")
	if len(parts) != 2 {
		return 0, 0, false
	}
	la, err1 := strconv.ParseFloat(parts[0], 64)
	lo, err2 := strconv.ParseFloat(parts[1], 64)
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return la, lo, true
}

func haversineKm(lat1, lon1, lat2, lon2 float64) float64 {
	const R = 6371.0
	toRad := func(d float64) float64 { return d * math.Pi / 180 }
	dLat := toRad(lat2 - lat1)
	dLon := toRad(lon2 - lon1)
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(toRad(lat1))*math.Cos(toRad(lat2))*math.Sin(dLon/2)*math.Sin(dLon/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return R * c
}
