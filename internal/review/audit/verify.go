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

// Verify attests the active log in two stages.
//
//  1. Chain walk (verifyRecords): every record's prev_hash must equal the
//     recomputed hash of its predecessor. This catches modification of any
//     non-tail record and any reordering, because an altered record changes its
//     hash and breaks the successor's link.
//
//  2. Head anchor (verifyHeadAnchor): the chain alone cannot attest its own last
//     link — the tail record has no successor storing its hash, so modifying,
//     truncating, or forging an extra tail record would pass stage 1. The head
//     sidecar pins the tail's count and hash, closing that gap. Every Append
//     advances it under the same lock that writes the record.
//
// A chain break takes precedence over a head mismatch. An empty log with no head
// verifies OK (a fresh, never-written log).
func (l *Log) Verify() (VerifyResult, error) {
	recs, err := l.Records()
	if err != nil {
		return VerifyResult{}, err
	}
	res, err := verifyRecords(recs)
	if err != nil {
		return VerifyResult{}, err
	}
	if !res.OK {
		return res, nil
	}
	head, hasHead, err := l.readHead()
	if err != nil {
		return VerifyResult{}, err
	}
	return verifyHeadAnchor(recs, head, hasHead)
}

// verifyHeadAnchor checks the tail record against the head sidecar. It runs only
// after the chain has verified, so any failure here is specifically a tail-level
// tamper the chain could not see.
func verifyHeadAnchor(recs []Record, head headAnchor, hasHead bool) (VerifyResult, error) {
	n := len(recs)
	if n == 0 {
		if hasHead {
			return brokenHead(0, 0, "head anchor is present but the log has no records (all records were truncated)"), nil
		}
		return VerifyResult{OK: true, Count: 0}, nil
	}
	if !hasHead {
		return brokenHead(n, n, "head anchor is missing; the tail record is unattested (anchor removed)"), nil
	}
	if head.Count != n {
		return brokenHead(n, n, fmt.Sprintf(
			"head anchor count %d does not match %d record(s) on disk (records were added or removed at the tail)",
			head.Count, n)), nil
	}
	tailHash, err := hashRecord(recs[n-1])
	if err != nil {
		return VerifyResult{}, err
	}
	if head.TailHash != tailHash {
		return brokenHead(n, n, fmt.Sprintf("tail record %d hash does not match the head anchor (the last record was modified)", n)), nil
	}
	return VerifyResult{OK: true, Count: n}, nil
}

// brokenHead renders a head-anchor verification failure.
func brokenHead(count, brokenAt int, reason string) VerifyResult {
	return VerifyResult{OK: false, Count: count, BrokenAt: brokenAt, Reason: reason}
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
