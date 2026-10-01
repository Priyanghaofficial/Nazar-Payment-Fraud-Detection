package contract

// ProfileBundle is the state read strictly before scoring an event (docs/02 §3.4:
// "the transaction being scored is never in its own features"). Every sub-bundle carries
// its own presence/degraded flag so a partial Redis outage degrades a slice of features
// rather than the whole decision (docs/00 §8, docs/01 §5).
type ProfileBundle struct {
	Payer PayerBundle
	Payee PayeeBundle
	Device DeviceBundle
	Pair  PairBundle
	ASN   ASNBundle

	// LoadedAtMs is when this bundle was assembled (monotonic-adjacent wall clock),
	// used to compute per-source staleness stamped onto the decision (D2).
	LoadedAtMs int64
}

type PayerBundle struct {
	Present bool
	Degraded bool // true if this group's Redis read failed / timed out

	AmtMedianMinor int64
	AmtMADMinor    int64
	AmtP95Minor    int64
	AmtP99Minor    int64
	HourHist       [24]float64 // normalised probability per hour-of-day bucket
	PayeeSetSize   int64
	DeviceSetSize  int64
	AccountAgeDays float64
	TxnCountLifetime int64
	Txn1hP999      float64 // baseline p999 of 1h txn count, for extreme-velocity rail
	BaselineVersion string
	BaselineUpdatedAtMs int64

	KnownPayees  map[string]bool
	KnownDevices map[string]bool
	KnownASNs    map[int32]bool

	LastTsMs      int64
	LastGeoCell   string
	LastAmtMinor  int64

	// Windowed counts, read via ZCOUNT — never ZCARD (docs/02 §3.3).
	TxnVelocity1m int64
	TxnVelocity5m int64
	TxnVelocity1h int64
	TxnVelocity24h int64

	// Windowed sums, read via bucketed HMGET (docs/02 §3.2).
	AmtSum1hMinor  int64
	AmtSum24hMinor int64
	AmtMean30dDailyMinor int64
}

type PayeeBundle struct {
	Present bool
	Degraded bool

	FirstSeenByUsAtMs int64 // D8: "we first saw this account", never "created"

	Fanin1h  int64 // ZCOUNT distinct payers, 1h window
	Fanin24h int64

	InflowSum1hMinor  int64
	OutflowSum1hMinor int64
	FwdLatencyP50Sec  float64
	FwdSampleN        int64
	FwdUpdatedAtMs    int64

	// per-payer share of 24h inflow, for HHI concentration
	PayerInflowShare24h map[string]float64
	DistinctPayerBanks1h int64
}

type DeviceBundle struct {
	Present bool
	Degraded bool

	FirstSeenAtMs int64
	AcctDegree24h int64 // ZCOUNT distinct accounts seen on this device, 24h
}

type PairBundle struct {
	Present bool // false means "no relationship" — pairs with txn_count_90d < 2 aren't stored
	Degraded bool

	TxnCount90d      int64
	AmtP95Minor      int64
	LastTsMs         int64
	LastDisposition  string // e.g. "CLEAN" | "FRAUD" | "UNKNOWN"
	FirstAddedAtMs   int64
	LastCreditorAccount string
}

type ASNBundle struct {
	Present bool
	Degraded bool

	AcctDegree1h int64
}
