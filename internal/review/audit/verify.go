package audit

import "fmt"

// VerifyResult is the outcome of attesting a log. OK is true when every chain
// link is intact and the head anchor raises no tamper alarm; otherwise BrokenAt
// names the 1-based index of the first record implicated and Reason describes
// the break.
//
// TornAppend flags the one benign inconsistency an interrupted Append can
// leave behind: the log holds exactly one valid, correctly-chained record more
// than the head anchor attests (the record landed, the anchor update did not).
// Chain integrity still holds, so OK stays true — but callers surfacing audit
// status should show the flag, and an operator heals it with RepairHead (or it
// self-heals on the next successful Append). Note the honest limit: a torn
// append is byte-for-byte indistinguishable from a single FORGED,
// correctly-chained record appended out-of-band, which is why the state is
// flagged rather than silently accepted. Any other head/log divergence
// (two or more records ahead, anchored record altered, records truncated)
// remains a hard tamper failure.
type VerifyResult struct {
	OK         bool
	Count      int
	BrokenAt   int
	Reason     string
	TornAppend bool
}

// Verify attests the active log in two stages. Both stages hash the EXACT
// stored line bytes (post-trim, as read), never a re-marshal of the decoded
// struct — so even a byte-level rewrite that preserves JSON semantics (key
// reordering, whitespace, escape or number reformatting) breaks attestation.
//
//  1. Chain walk (verifyRecords): every record's prev_hash must equal the
//     SHA-256 of its predecessor's stored line. This catches modification of
//     any non-tail record and any reordering.
//
//  2. Head anchor (verifyHeadAnchor): the chain alone cannot attest its own
//     last link — the tail line has no successor storing its hash, so
//     modifying, truncating, or forging an extra tail record would pass the
//     chain walk. The head sidecar pins the tail line's count and hash,
//     closing that gap. Every Append advances it under the same lock that
//     writes the line.
//
// A chain break takes precedence over a head mismatch. An empty log with no head
// verifies OK (a fresh, never-written log). A log exactly one clean record ahead
// of its anchor verifies OK with TornAppend set (see VerifyResult).
//
// Verify takes no lock; a result observed concurrently with a live Append may
// transiently show TornAppend.
func (l *Log) Verify() (VerifyResult, error) {
	res, _, err := l.verifyState()
	return res, err
}

// verifyState is the shared core of Verify and RepairHead: it loads the stored
// lines, walks the chain, checks the head anchor, and returns the raw lines
// alongside the result so RepairHead can re-anchor without a second read.
func (l *Log) verifyState() (VerifyResult, [][]byte, error) {
	recs, raws, err := l.readStored()
	if err != nil {
		return VerifyResult{}, nil, err
	}
	if res := verifyRecords(recs, raws); !res.OK {
		return res, raws, nil
	}
	head, hasHead, err := l.readHead()
	if err != nil {
		return VerifyResult{}, nil, err
	}
	return verifyHeadAnchor(raws, head, hasHead), raws, nil
}

// verifyHeadAnchor checks the stored tail line against the head sidecar. It
// runs only after the chain has verified, so any failure here is specifically
// a tail-level inconsistency the chain could not see. The
// exactly-one-clean-record-ahead shape is classified as a torn append (see
// VerifyResult.TornAppend); everything else that diverges is tamper.
func verifyHeadAnchor(raws [][]byte, head headAnchor, hasHead bool) VerifyResult {
	n := len(raws)
	// Treat a missing sidecar as an anchor attesting zero records: a fresh log
	// is OK, a single chained record is a torn FIRST append (the head write
	// never happened), and two or more unanchored records are tamper.
	if !hasHead {
		head = headAnchor{Count: 0, TailHash: ""}
	}
	if n == 0 {
		if head.Count != 0 {
			return brokenHead(0, 0, "head anchor is present but the log has no records (all records were truncated)")
		}
		return VerifyResult{OK: true, Count: 0}
	}
	switch {
	case head.Count == n:
		return verifyAnchoredTail(raws, head)
	case head.Count == n-1:
		return verifyTornCandidate(raws, head)
	default:
		return brokenHead(n, n, fmt.Sprintf(
			"head anchor count %d does not match %d record(s) on disk (records were added or removed at the tail)",
			head.Count, n))
	}
}

// verifyAnchoredTail handles the exact-match shape (head.Count == len(raws)):
// the anchor must equal the hash of the stored tail line, else the last record
// was modified.
func verifyAnchoredTail(raws [][]byte, head headAnchor) VerifyResult {
	n := len(raws)
	if head.TailHash != hashBytes(raws[n-1]) {
		return brokenHead(n, n, fmt.Sprintf("tail record %d hash does not match the head anchor (the last record was modified)", n))
	}
	return VerifyResult{OK: true, Count: n}
}

// verifyTornCandidate handles the head-exactly-one-behind shape. It is benign
// (TornAppend) only if the stored line the anchor DOES attest is intact — for a
// torn first append there is no anchored line to check. The chain walk already
// proved the extra record chains onto its predecessor.
func verifyTornCandidate(raws [][]byte, head headAnchor) VerifyResult {
	n := len(raws)
	if head.Count > 0 && head.TailHash != hashBytes(raws[head.Count-1]) {
		return brokenHead(n, head.Count, fmt.Sprintf(
			"head anchor does not match record %d it claims to attest (record %d was modified)",
			head.Count, head.Count))
	}
	return VerifyResult{
		OK:         true,
		Count:      n,
		TornAppend: true,
		Reason: fmt.Sprintf(
			"log has %d record(s) but the head anchor attests %d: an append was interrupted before the anchor advanced (or a single chained record was appended out-of-band); run RepairHead after review",
			n, head.Count),
	}
}

// brokenHead renders a head-anchor verification failure.
func brokenHead(count, brokenAt int, reason string) VerifyResult {
	return VerifyResult{OK: false, Count: count, BrokenAt: brokenAt, Reason: reason}
}

// verifyRecords is the pure core of the chain walk, separated so it can be
// unit-tested against in-memory records without touching the filesystem. recs
// and raws are lockstep views of the same stored lines: recs supplies each
// record's claimed prev_hash, raws supplies the exact bytes whose hash the NEXT
// record must claim.
func verifyRecords(recs []Record, raws [][]byte) VerifyResult {
	expected := GenesisPrevHash
	for i, r := range recs {
		if r.PrevHash != expected {
			return VerifyResult{
				OK:       false,
				Count:    len(recs),
				BrokenAt: i + 1,
				Reason:   brokenReason(i, r.PrevHash, expected),
			}
		}
		expected = hashBytes(raws[i])
	}
	return VerifyResult{OK: true, Count: len(recs)}
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
