package profile

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

func scoreStr(ms int64) string { return strconv.FormatInt(ms, 10) }

func minuteEpochStr(ms int64) string { return strconv.FormatInt(ms/60_000, 10) }
func hourEpochStr(ms int64) string   { return strconv.FormatInt(ms/3_600_000, 10) }
func dayEpochStr(ms int64) string    { return strconv.FormatInt(ms/86_400_000, 10) }

func minuteBucketKeys(nowMs int64, n int) []string {
	cur := nowMs / 60_000
	keys := make([]string, n)
	for i := 0; i < n; i++ {
		keys[i] = strconv.FormatInt(cur-int64(i), 10)
	}
	return keys
}

func hourBucketKeys(nowMs int64, n int) []string {
	cur := nowMs / 3_600_000
	keys := make([]string, n)
	for i := 0; i < n; i++ {
		keys[i] = strconv.FormatInt(cur-int64(i), 10)
	}
	return keys
}

func dayBucketKeys(nowMs int64, n int) []string {
	cur := nowMs / 86_400_000
	keys := make([]string, n)
	for i := 0; i < n; i++ {
		keys[i] = strconv.FormatInt(cur-int64(i), 10)
	}
	return keys
}

func hourOfDayUTC(ms int64) string {
	t := time.UnixMilli(ms).UTC()
	return strconv.Itoa(t.Hour())
}

// sumHMGet sums an HMGet result, treating nils/unparseable entries as zero. This is the
// in-process sum-over-range that Redis itself cannot do for a hash (docs/02 §3.2).
func sumHMGet(cmd *redis.SliceCmd) int64 {
	res, err := cmd.Result()
	if err != nil {
		return 0
	}
	var sum int64
	for _, v := range res {
		if v == nil {
			continue
		}
		s, ok := v.(string)
		if !ok {
			continue
		}
		sum += parseInt64(s)
	}
	return sum
}

func parseInt64(s string) int64 {
	if s == "" {
		return 0
	}
	v, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		f, ferr := strconv.ParseFloat(s, 64)
		if ferr == nil {
			return int64(f)
		}
		return 0
	}
	return v
}

func parseFloat(s string) float64 {
	if s == "" {
		return 0
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0
	}
	return v
}

func percentile(sortedVals []float64, p float64) float64 {
	if len(sortedVals) == 0 {
		return 0
	}
	idx := int(p * float64(len(sortedVals)-1))
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sortedVals) {
		idx = len(sortedVals) - 1
	}
	return sortedVals[idx]
}

func membersOf(ctx context.Context, rdb *redis.Client, key string) map[string]bool {
	members, err := rdb.SMembers(ctx, key).Result()
	out := make(map[string]bool, len(members))
	if err != nil {
		return out
	}
	for _, m := range members {
		out[m] = true
	}
	return out
}

func asnMembersOf(ctx context.Context, rdb *redis.Client, key string) map[int32]bool {
	members, err := rdb.SMembers(ctx, key).Result()
	out := make(map[int32]bool, len(members))
	if err != nil {
		return out
	}
	for _, m := range members {
		if v, err := strconv.Atoi(m); err == nil {
			out[int32(v)] = true
		}
	}
	return out
}

// bankOfAccount derives the issuing participant from an account identifier. P0 convention
// (see py/generator): accounts are minted as "<BANK_ID>-<serial>", e.g. "BANK_A-000123".
// This is a generator convention, not a protocol requirement — a real deployment would
// resolve this via IFSC/participant registry lookup (docs/02 §1, bank_instance field).
func bankOfAccount(acct string) string {
	if i := strings.Index(acct, "-"); i > 0 {
		return acct[:i]
	}
	return "UNKNOWN"
}

// encodeHourHist / decodeHourHist pack the 24-bucket hour histogram as base64 of 24
// big-endian uint32 counts, stored as a single hash field (docs/02 §3.1: "hour_hist_b64").
func encodeHourHist(counts map[string]string) string {
	buf := make([]byte, 24*4)
	for h := 0; h < 24; h++ {
		v := parseInt64(counts[strconv.Itoa(h)])
		binary.BigEndian.PutUint32(buf[h*4:], uint32(v))
	}
	return base64.StdEncoding.EncodeToString(buf)
}

func decodeHourHist(b64 string) [24]float64 {
	var hist [24]float64
	buf, err := base64.StdEncoding.DecodeString(b64)
	if err != nil || len(buf) != 24*4 {
		// cold start: uniform prior, Laplace-smoothed (docs/02 §4.1 hour_surprisal)
		for h := range hist {
			hist[h] = 1.0 / 24.0
		}
		return hist
	}
	var total float64
	counts := make([]float64, 24)
	for h := 0; h < 24; h++ {
		v := binary.BigEndian.Uint32(buf[h*4:])
		counts[h] = float64(v) + 1 // Laplace smoothing, alpha=1
		total += counts[h]
	}
	for h := 0; h < 24; h++ {
		hist[h] = counts[h] / total
	}
	return hist
}
