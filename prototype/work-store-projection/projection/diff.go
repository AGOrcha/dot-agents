package projection

import (
	"fmt"
	"strings"
)

// UnifiedDiff returns a minimal line-oriented diff of a vs b (added/removed
// lines), enough to show round-trip churn in the demo. Empty when identical.
// This is a small LCS-free diff: it is not a full Myers diff, but for the
// near-identical regen-vs-orig case it surfaces exactly the changed lines.
func UnifiedDiff(a, b []byte) string {
	if string(a) == string(b) {
		return ""
	}
	la := strings.Split(string(a), "\n")
	lb := strings.Split(string(b), "\n")
	var sb strings.Builder
	common := lcs(la, lb)
	ia, ib := 0, 0
	for _, c := range common {
		for ia < len(la) && la[ia] != c {
			fmt.Fprintf(&sb, "- %s\n", la[ia])
			ia++
		}
		for ib < len(lb) && lb[ib] != c {
			fmt.Fprintf(&sb, "+ %s\n", lb[ib])
			ib++
		}
		ia++
		ib++
	}
	for ; ia < len(la); ia++ {
		fmt.Fprintf(&sb, "- %s\n", la[ia])
	}
	for ; ib < len(lb); ib++ {
		fmt.Fprintf(&sb, "+ %s\n", lb[ib])
	}
	return sb.String()
}

// DiffLineCount returns the number of changed (added+removed) lines between a
// and b — the churn metric used to quantify naive vs canonical serializers.
func DiffLineCount(a, b []byte) int {
	d := UnifiedDiff(a, b)
	if d == "" {
		return 0
	}
	n := 0
	for _, line := range strings.Split(d, "\n") {
		if strings.HasPrefix(line, "+ ") || strings.HasPrefix(line, "- ") {
			n++
		}
	}
	return n
}

// lcs returns a longest common subsequence of two string slices.
func lcs(a, b []string) []string {
	n, m := len(a), len(b)
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}
	var out []string
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			out = append(out, a[i])
			i++
			j++
		case dp[i+1][j] >= dp[i][j+1]:
			i++
		default:
			j++
		}
	}
	return out
}
