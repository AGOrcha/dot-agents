package scoring

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"

	"github.com/NikashPrakash/dot-agents/internal/fsops"
	"go.yaml.in/yaml/v3"
)

// PersistedScore is the on-disk YAML shape for one iteration's score.
// It is the durable record consumed by R1's CLI and (later) the R2 dashboard,
// so the shape is explicit and stable rather than reusing the in-memory Score
// directly. The breakdown is preserved row-per-row in rubric order so that
// renderers do not need access to the rubric to reproduce a stable display.
type PersistedScore struct {
	Iteration     int                     `yaml:"iteration"`
	RubricVersion string                  `yaml:"rubric_version"`
	Scored        bool                    `yaml:"scored"`
	Value         float64                 `yaml:"value"`
	Band          string                  `yaml:"band"`
	Breakdown     []PersistedContribution `yaml:"breakdown"`
	// LinkedTracesToOutcomes is the derived legacy iteration-log marker:
	// true when verifier.linked_traces names at least one trace ↔ outcome pair.
	// Computed by DeriveLinkedTracesToOutcomes; populated by BuildPersistedScore.
	// Omitted from the YAML when the score was written without an IterationRecord.
	LinkedTracesToOutcomes bool `yaml:"linked_traces_to_outcomes,omitempty"`
}

// PersistedContribution is one row of the per-signal breakdown as written to disk.
type PersistedContribution struct {
	Signal          SignalID `yaml:"signal"`
	Label           string   `yaml:"label"`
	Present         bool     `yaml:"present"`
	SubScore        float64  `yaml:"sub_score"`
	Detail          string   `yaml:"detail,omitempty"`
	NominalWeight   float64  `yaml:"nominal_weight"`
	EffectiveWeight float64  `yaml:"effective_weight"`
	Contribution    float64  `yaml:"contribution"`
}

// SessionScore is the per-session aggregate written alongside the per-iteration
// scores. Value is the mean of the scored per-iteration values for the session;
// unscored iterations drop out of the average — the same "absent does not vote"
// rule the rubric uses for absent signals.
//
// A session with no scored iterations is itself unscored: Scored=false, Value=0,
// Band=BandUnscored.
type SessionScore struct {
	SessionID     string           `yaml:"session_id"`
	RubricVersion string           `yaml:"rubric_version"`
	Iterations    []int            `yaml:"iterations"`
	Scored        bool             `yaml:"scored"`
	Value         float64          `yaml:"value"`
	Band          string           `yaml:"band"`
	PerIteration  []SessionIterRef `yaml:"per_iteration"`
}

// SessionIterRef carries enough per-iteration detail to render a session view
// without having to fan out and read every iter-N.score.yaml sidecar.
type SessionIterRef struct {
	Iteration int     `yaml:"iteration"`
	Scored    bool    `yaml:"scored"`
	Value     float64 `yaml:"value"`
	Band      string  `yaml:"band"`
}

// IterationScorePath returns the sidecar path for an iteration's score: the
// iter-N.score.yaml file adjacent to iter-N.yaml in the iteration log dir.
func IterationScorePath(iterLogDir string, iteration int) string {
	return filepath.Join(iterLogDir, fmt.Sprintf("iter-%d.score.yaml", iteration))
}

// SessionScorePath returns the sidecar path for a session's score, named by
// the session_id so distinct sessions never collide. Callers must pass a
// non-empty session id; empty ids are not addressable on disk.
func SessionScorePath(iterLogDir, sessionID string) (string, error) {
	if sessionID == "" {
		return "", errors.New("scoring: empty session_id has no addressable sidecar")
	}
	return filepath.Join(iterLogDir, fmt.Sprintf("session-%s.score.yaml", sessionID)), nil
}

// WriteIterationScore persists a single iteration score to its sidecar path
// and returns the written path. The file is written atomically: a temp file
// in the same directory is renamed into place so concurrent readers never see
// a partially-written sidecar.
func WriteIterationScore(iterLogDir string, score Score) (string, error) {
	path := IterationScorePath(iterLogDir, score.Iteration)
	if err := writeYAMLAtomic(path, toPersistedScore(score)); err != nil {
		return "", fmt.Errorf("scoring: write iter-%d score: %w", score.Iteration, err)
	}
	return path, nil
}

// WriteIterationScores persists every per-iteration score and returns the
// written paths in input order. The first write failure returns immediately
// with the paths written up to that point — the caller decides whether to
// retry or roll back.
func WriteIterationScores(iterLogDir string, scores []Score) ([]string, error) {
	paths := make([]string, 0, len(scores))
	for _, s := range scores {
		p, err := WriteIterationScore(iterLogDir, s)
		if err != nil {
			return paths, err
		}
		paths = append(paths, p)
	}
	return paths, nil
}

// WriteSessionScore persists a session aggregate to its sidecar path.
func WriteSessionScore(iterLogDir string, ss SessionScore) (string, error) {
	path, err := SessionScorePath(iterLogDir, ss.SessionID)
	if err != nil {
		return "", err
	}
	if err := writeYAMLAtomic(path, ss); err != nil {
		return "", fmt.Errorf("scoring: write session-%s score: %w", ss.SessionID, err)
	}
	return path, nil
}

// AggregateSessions groups the scores by their iteration's session_id and
// produces one SessionScore per session.
//
// records and scores must align by iteration order (the order BuildSignalSets
// and ScoreAll return). Iterations whose record carries an empty session_id
// are silently skipped — an unaddressable session cannot be persisted as a
// sidecar, and the proposal mandates per-session addressability.
//
// Output is sorted by SessionID for deterministic iteration / diffing.
func AggregateSessions(r Rubric, records []IterationRecord, scores []Score) []SessionScore {
	if len(records) != len(scores) {
		return nil
	}

	type bucket struct {
		iters      []SessionIterRef
		sumScored  float64
		numScored  int
		iterations []int
	}
	bySession := make(map[string]*bucket)
	order := make([]string, 0)

	for i, rec := range records {
		sid := rec.Agent.SessionID
		if sid == "" {
			continue
		}
		b, ok := bySession[sid]
		if !ok {
			b = &bucket{}
			bySession[sid] = b
			order = append(order, sid)
		}
		s := scores[i]
		b.iterations = append(b.iterations, s.Iteration)
		b.iters = append(b.iters, SessionIterRef{
			Iteration: s.Iteration,
			Scored:    s.Scored,
			Value:     s.Value,
			Band:      s.Band,
		})
		if s.Scored {
			b.sumScored += s.Value
			b.numScored++
		}
	}

	sort.Strings(order)
	out := make([]SessionScore, 0, len(order))
	for _, sid := range order {
		b := bySession[sid]
		ss := SessionScore{
			SessionID:     sid,
			RubricVersion: r.Version,
			Iterations:    b.iterations,
			PerIteration:  b.iters,
		}
		if b.numScored == 0 {
			ss.Band = BandUnscored
		} else {
			ss.Scored = true
			ss.Value = b.sumScored / float64(b.numScored)
			ss.Band = r.Band(ss.Value)
		}
		out = append(out, ss)
	}
	return out
}

// WriteSessionScores persists every session aggregate and returns the written
// paths in input order.
func WriteSessionScores(iterLogDir string, sessions []SessionScore) ([]string, error) {
	paths := make([]string, 0, len(sessions))
	for _, ss := range sessions {
		p, err := WriteSessionScore(iterLogDir, ss)
		if err != nil {
			return paths, err
		}
		paths = append(paths, p)
	}
	return paths, nil
}

func toPersistedScore(s Score) PersistedScore {
	rows := make([]PersistedContribution, len(s.Breakdown))
	for i, r := range s.Breakdown {
		rows[i] = PersistedContribution{
			Signal:          r.Signal,
			Label:           r.Label,
			Present:         r.Present,
			SubScore:        r.SubScore,
			Detail:          r.Detail,
			NominalWeight:   r.NominalWeight,
			EffectiveWeight: r.EffectiveWeight,
			Contribution:    r.Contribution,
		}
	}
	return PersistedScore{
		Iteration:     s.Iteration,
		RubricVersion: s.RubricVersion,
		Scored:        s.Scored,
		Value:         s.Value,
		Band:          s.Band,
		Breakdown:     rows,
	}
}

func writeYAMLAtomic(path string, v any) error {
	data, err := yaml.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	return fsops.WriteFileAtomic(path, data)
}
