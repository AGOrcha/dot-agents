package audit

import "fmt"

// VerifyResult is the outcome of walking a log's hash chain. OK is true only
// when every link is intact; otherwise BrokenAt names the 1-based index of the
// first record whose prev_hash does not match the recomputed hash of its
// predecessor (or, for the first record, does not equal GenesisPrevHash), and
// Reason describes the break.
type VerifyResult struct {
	OK       bool
	Count    int
	BrokenAt int
	Reason   string
}

// Verify walks the active log's hash chain from the genesis record and reports
// the first integrity break, if any. A record whose content was altered changes
// its recomputed hash, so the break surfaces at the link that stored the old
// hash — the tamper is caught even though no record carries a self-hash.
//
// An empty log verifies OK (a chain of zero records has no broken links).
func (l *Log) Verify() (VerifyResult, error) {
	recs, err := l.Records()
	if err != nil {
		return VerifyResult{}, err
	}
	return verifyRecords(recs)
}

// verifyRecords is the pure core of Verify, separated so it can be unit-tested
// against in-memory records without touching the filesystem.
func verifyRecords(recs []Record) (VerifyResult, error) {
	expected := GenesisPrevHash
	for i, r := range recs {
		if r.PrevHash != expected {
			return VerifyResult{
				OK:       false,
				Count:    len(recs),
				BrokenAt: i + 1,
				Reason:   brokenReason(i, r.PrevHash, expected),
			}, nil
		}
		h, err := hashRecord(r)
		if err != nil {
			return VerifyResult{}, err
		}
		expected = h
	}
	return VerifyResult{OK: true, Count: len(recs)}, nil
}

// brokenReason renders a human-readable explanation of a chain break at the
// 1-based record index i+1.
func brokenReason(i int, got, want string) string {
	if i == 0 {
		return fmt.Sprintf("record 1 prev_hash %q is not the genesis hash", got)
	}
	return fmt.Sprintf(
		"record %d prev_hash %q does not match record %d hash %q (record %d was altered or removed)",
		i+1, got, i, want, i,
	)
}
