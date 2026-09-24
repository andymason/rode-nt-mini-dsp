package protocol

import (
	"bufio"
	"embed"
	"fmt"
	"strconv"
	"strings"
)

// The seven DSP lookup tables, committed under luts/. Each is 256 entries of
// uint32, one per line in hex. docs/re/README.md describes how they were
// recovered and docs/re/encoders.md how they are indexed.
//
// These replaced an earlier set reconstructed from USB captures, which covered
// only 40-91 of the 256 entries and interpolated between them. RØDE Connect does
// no interpolation at all: it truncates a scaled float to an index and reads one
// entry.
//
//go:embed luts/*.hex
var lutFS embed.FS

const lutEntries = 256

var (
	// CompThresholdTable is indexed by (1 - (dB+60)/60) * 255, so it runs
	// backwards relative to the dB value.
	CompThresholdTable = mustLoadTable("comp_threshold")

	// CompAttackTable is indexed by log(ms/0.1)/log(100) * 255.
	CompAttackTable = mustLoadTable("comp_attack")

	// CompReleaseTable is indexed by log(ms/5)/log(40) * 255.
	CompReleaseTable = mustLoadTable("comp_release")

	// CompGainTable is indexed by (dB/9) * 255.
	CompGainTable = mustLoadTable("comp_gain")

	// HarmonicsDriveTable is shared by Aural Exciter harmonics and Big Bottom
	// drive, both indexed directly by percentage.
	HarmonicsDriveTable = mustLoadTable("harmonics_drive")

	// AETune1Table and AETune2Table are both indexed by the same Aural Exciter
	// tune index.
	AETune1Table = mustLoadTable("ae_tune_1")
	AETune2Table = mustLoadTable("ae_tune_2")
)

// mustLoadTable parses luts/<name>.hex at package init. A failure here means the
// embedded data is corrupt, which no caller could sensibly recover from.
func mustLoadTable(name string) *[lutEntries]uint32 {
	t, err := loadTable(name)
	if err != nil {
		panic(fmt.Sprintf("protocol: loading LUT %q: %v", name, err))
	}
	return t
}

// loadTable reads one .hex file: a '#'-commented header followed by
// exactly lutEntries lines of 8-digit hex, one uint32 each, in table order.
func loadTable(name string) (*[lutEntries]uint32, error) {
	f, err := lutFS.Open("luts/" + name + ".hex")
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var table [lutEntries]uint32
	n := 0

	sc := bufio.NewScanner(f)
	for lineNo := 1; sc.Scan(); lineNo++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if n >= lutEntries {
			return nil, fmt.Errorf("more than %d entries (line %d)", lutEntries, lineNo)
		}
		v, err := strconv.ParseUint(line, 16, 32)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNo, err)
		}
		table[n] = uint32(v)
		n++
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if n != lutEntries {
		return nil, fmt.Errorf("got %d entries, want %d", n, lutEntries)
	}
	return &table, nil
}
