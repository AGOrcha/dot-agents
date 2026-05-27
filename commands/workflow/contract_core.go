package workflow

import (
	"fmt"
	"time"
)

// Shared business logic for materializing a DelegationContract on disk.
//
// Both `da workflow contract create` (orchestrator-owned direct work) and
// `da workflow fanout` (sub-agent delegation) write a contract YAML to the
// same directory using the same schema. Per #131 review, the per-CLI surfaces
// stay distinct, but the contract-writing business logic must be one path:
// each command resolves its mode-specific inputs (task, write scope, summary)
// then calls materializeDelegationContract to do the actual construction +
// persistence. This keeps `contract create` and `fanout` honest about how
// contracts are written and prevents the two from drifting apart.

// materializeContractRequest carries the resolved inputs needed to build
// and persist a DelegationContract. Each caller is responsible for resolving
// its own mode-specific values (e.g. write scope, summary text) before
// invoking the shared core; the core is intentionally narrow and does not
// reach back into TASKS.yaml or CLI flags.
type materializeContractRequest struct {
	ProjectPath     string
	Mode            DelegationContractMode
	PlanID          string
	TaskID          string
	Title           string
	Summary         string
	WriteScope      []string
	SuccessCriteria string
	Owner           string
	Now             time.Time
}

// materializeDelegationContract constructs a DelegationContract from the
// request, persists it via saveDelegationContract, and returns the built
// value to the caller. The returned pointer is the persisted contract; its
// UpdatedAt is set by saveDelegationContract during the write.
func materializeDelegationContract(req materializeContractRequest) (*DelegationContract, error) {
	now := req.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	createdAt := now.UTC().Format(time.RFC3339)
	contract := &DelegationContract{
		SchemaVersion:   1,
		ID:              fmt.Sprintf("del-%s-%d", req.TaskID, now.Unix()),
		Mode:            req.Mode,
		ParentPlanID:    req.PlanID,
		ParentTaskID:    req.TaskID,
		Title:           req.Title,
		Summary:         req.Summary,
		WriteScope:      append([]string(nil), req.WriteScope...),
		SuccessCriteria: req.SuccessCriteria,
		Owner:           req.Owner,
		Status:          "active",
		CreatedAt:       createdAt,
		UpdatedAt:       createdAt,
	}
	if err := saveDelegationContract(req.ProjectPath, contract); err != nil {
		return nil, fmt.Errorf("save delegation contract: %w", err)
	}
	return contract, nil
}
