package main

import (
	"fmt"
	"hash/fnv"
	"math"
	"strconv"
	"strings"
)

// Geography for the demo, matching py/generator/population.py's CITIES exactly so a decision
// made from generated data and one made from live simulated traffic land in the same place.
// geo_cell is "lat:lon" (go/internal/features/compute.go:parseGeoCell).
//
// This exists so the console can answer "where is the fraud?" from decisions the system
// actually made, rather than from a picture of a map. Every account keeps a stable home city
// derived from its own identifier, so the same payer does not teleport between payments —
// which would make the location signal meaningless and the heatmap a lie.
type city struct {
	Name string
	Lat  float64
	Lon  float64
}

var cities = []city{
	{"Bangalore", 12.9716, 77.5946},
	{"Mumbai", 19.0760, 72.8777},
	{"Delhi", 28.7041, 77.1025},
	{"Chennai", 13.0827, 80.2707},
	{"Hyderabad", 17.3850, 78.4867},
	{"Pune", 18.5204, 73.8567},
	{"Kolkata", 22.5726, 88.3639},
	{"Ahmedabad", 23.0225, 72.5714},
}

// homeCity picks a stable city for an account. Deterministic: the same account always
// resolves to the same place, across restarts.
func homeCity(account string) city {
	h := fnv.New32a()
	_, _ = h.Write([]byte(account))
	return cities[int(h.Sum32())%len(cities)]
}

// geoCellFor returns the "lat:lon" cell for an account, jittered slightly within its city so
// two payers in the same city are not at an identical point.
func geoCellFor(account string) string {
	c := homeCity(account)
	h := fnv.New32a()
	_, _ = h.Write([]byte(account + "#jitter"))
	j := float64(int(h.Sum32())%200-100) / 10000.0 // ±0.01 degrees, ~1km
	return fmt.Sprintf("%.4f:%.4f", c.Lat+j, c.Lon+j)
}

// cityOf maps a stored geo_cell back to the nearest known city. Cells that are not "lat:lon"
// — the deliberately far-away cell the account-takeover campaign uses, for instance — return
// false so the caller can report them as unknown rather than silently binning them into
// whichever city happens to be closest to nothing.
func cityOf(geoCell string) (string, bool) {
	lat, lon, ok := parseCell(geoCell)
	if !ok {
		return "", false
	}
	best, bestD := "", math.MaxFloat64
	for _, c := range cities {
		d := (c.Lat-lat)*(c.Lat-lat) + (c.Lon-lon)*(c.Lon-lon)
		if d < bestD {
			best, bestD = c.Name, d
		}
	}
	// Beyond roughly 2 degrees the point is not in any city we model; saying so is more
	// honest than assigning it to the least-distant one.
	if bestD > 4.0 {
		return "", false
	}
	return best, true
}

func parseCell(s string) (float64, float64, bool) {
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return 0, 0, false
	}
	lat, err1 := strconv.ParseFloat(parts[0], 64)
	lon, err2 := strconv.ParseFloat(parts[1], 64)
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return lat, lon, true
}

func cityLatLon(name string) (float64, float64) {
	for _, c := range cities {
		if c.Name == name {
			return c.Lat, c.Lon
		}
	}
	return 0, 0
}
