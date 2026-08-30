package protocol

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var updateBaseline = flag.Bool("update-baseline", false,
	"regenerate internal/protocol/testdata/baseline_v1.csv from the current encoders")

const baselinePath = "testdata/baseline_v1.csv"

// The baseline is a dense sweep of every parameter through the current encoders.
// It is not a correctness oracle — the encoders are known to be approximate (see
// capture_oracle_test.go). It is a diff surface: when the real 256-entry tables
// replace the capture-derived ones, regenerating this file shows exactly which
// UI values changed and by how much, which is the review artifact for that swap.
//
// Regenerate with:
//
//	go test ./internal/protocol/ -run TestBaseline -update-baseline

type sweep struct {
	effect   string
	param    string
	min, max float64
	steps    int
	fn       func(float64) []byte
}

func sweeps() []sweep {
	return []sweep{
		{"compressor", "threshold", -60, 0, 120, EncodeCompThreshold},
		{"compressor", "ratio", 1.5, 4.5, 30, EncodeCompRatio},
		{"compressor", "attack", 0.1, 10, 99, EncodeCompAttack},
		{"compressor", "release", 5, 200, 195, EncodeCompRelease},
		{"compressor", "gain", 0, 9, 90, EncodeCompGain},
		{"noise_gate", "threshold", -60, 0, 120, EncodeNGThreshold},
		{"noise_gate", "attack", 0.1, 1000, 200, EncodeNGAttack},
		{"noise_gate", "hold", 50, 2000, 195, EncodeNGHold},
		{"noise_gate", "release", 50, 2000, 195, EncodeNGRelease},
		{"noise_gate", "range", -100, 0, 200, EncodeNGRange},
		{"noise_gate", "hysteresis", 0, 100, 100, EncodeNGHysteresis},
		{"aural_exciter", "harmonics", 0, 100, 200, EncodeAEHarmonics},
		{"aural_exciter", "tune", 600, 5000, 220, EncodeAETune},
		{"big_bottom", "drive", 0, 100, 200, EncodeBBDrive},
		{"big_bottom", "tune", 60, 312, 252, EncodeBBTune},
	}
}

func generateBaseline() []string {
	lines := []string{"effect,param,ui_value,payload_hex"}
	for _, s := range sweeps() {
		span := s.max - s.min
		for i := 0; i <= s.steps; i++ {
			v := s.min + span*float64(i)/float64(s.steps)
			lines = append(lines, fmt.Sprintf("%s,%s,%.4f,%x", s.effect, s.param, v, s.fn(v)))
		}
	}
	return lines
}

func TestBaseline(t *testing.T) {
	got := generateBaseline()

	if *updateBaseline {
		if err := os.MkdirAll(filepath.Dir(baselinePath), 0o755); err != nil {
			t.Fatalf("creating testdata dir: %v", err)
		}
		out := strings.Join(got, "\n") + "\n"
		if err := os.WriteFile(baselinePath, []byte(out), 0o644); err != nil {
			t.Fatalf("writing baseline: %v", err)
		}
		t.Logf("wrote %s (%d rows)", baselinePath, len(got)-1)
		return
	}

	f, err := os.Open(baselinePath)
	if err != nil {
		t.Fatalf("opening baseline (regenerate with -update-baseline): %v", err)
	}
	defer func() { _ = f.Close() }()

	var want []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		want = append(want, sc.Text())
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("reading baseline: %v", err)
	}

	if len(got) != len(want) {
		t.Fatalf("baseline has %d rows, encoders now produce %d — regenerate with -update-baseline",
			len(want)-1, len(got)-1)
	}

	diffs := 0
	for i := range got {
		if got[i] != want[i] {
			diffs++
			if diffs <= 10 {
				t.Errorf("line %d:\n  baseline: %s\n  current:  %s", i+1, want[i], got[i])
			}
		}
	}
	if diffs > 10 {
		t.Errorf("... and %d more differing lines", diffs-10)
	}
	if diffs > 0 {
		t.Logf("%d of %d rows changed; if intended, regenerate with -update-baseline",
			diffs, len(got)-1)
	}
}
