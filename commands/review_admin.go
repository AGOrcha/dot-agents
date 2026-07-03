// review_admin.go adds the R5 admin surface under the existing `da review`
// group (r5-review-labeling-access, admin-cli task): `da review users
// {add,list,remove,set-role}` for RBAC user management (spec D5.3 / R9) and
// `da review audit {view,verify,repair}` for the hash-chained audit log
// (spec D5.4 / R7). The naming decision from the plan is pinned here: the
// admin subtree nests under the existing `da review` group because users/audit
// do not collide with the proposal subcommands show/approve/reject.
//
// Every mutating action (user create, user delete, role change) writes one
// audit record with the same FAIL-CLOSED discipline as internal/review/http's
// mutation guard (spec R6): the whole [load → mutate → save → audit] section
// runs under the agentslock file lock on the users file — the same lock the
// HTTP handlers take, so CLI processes and a running review service serialize
// their read-modify-write cycles — with the users file's pre-image captured up
// front. If the audit append fails, the users file is rolled back and the
// command fails; the mutation does not survive unaudited.
//
// Token issuance is print-once (spec OQ1): the plaintext token appears exactly
// once in the `users add` output; only its argon2id hash persists.
package commands

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/AGOrcha/dot-agents/internal/agentslock"
	"github.com/AGOrcha/dot-agents/internal/fsops"
	"github.com/AGOrcha/dot-agents/internal/review/audit"
	"github.com/AGOrcha/dot-agents/internal/review/auth"
	"github.com/spf13/cobra"
)

// defaultReviewAuditLog is the repo-relative audit log path (design D5.4),
// mirroring defaultIterLogDir's repo-relative convention in score.go.
const defaultReviewAuditLog = ".agents/active/review/audit.log.jsonl"

// reviewTokenEnv is the environment variable consulted for the admin bearer
// token when --token is not passed. An env var keeps token plaintext out of
// shell history by default.
const reviewTokenEnv = "DA_REVIEW_TOKEN"

// reviewRoleFlag is the shared --role flag name on `users add` / `users
// set-role`.
const reviewRoleFlag = "role"

// reviewBeforeYearFlag is the --before-year flag name on `audit prune`.
const reviewBeforeYearFlag = "before-year"

// reviewBootstrapActor is the audit actor recorded when the first admin is
// created against an empty users file (no admin exists yet to authenticate).
const reviewBootstrapActor = "bootstrap"

// fmtReviewUsersRow / fmtReviewAuditRow are the shared table row formats so
// header and rows can never drift apart.
const (
	fmtReviewUsersRow = "%-32s  %-9s  %-22s  %s\n"
	fmtReviewAuditRow = "%-20s  %-24s  %-9s  %-13s  %s\n"
)

// reviewAdminDeps is the narrow collaborator surface the review admin
// commands need (interface-DI per docs/TEST_SEAMS.md, mirroring reviewDeps in
// review.go). One interface covers the users-file store, the authenticator,
// the cross-process lock, the fail-closed rollback touch points, and the
// audit log, so the whole admin pipeline has a single fault-injection
// surface. File-scoped — do not share with other commands files.
type reviewAdminDeps interface {
	DefaultUsersPath() (string, error)
	LoadUsers(path string) (*auth.UsersFile, error)
	SaveUsers(uf *auth.UsersFile, path string) error
	Authenticate(usersPath, token string) (auth.Identity, error)
	AcquireFileLock(path string) (release func() error, err error)
	ReadFile(path string) ([]byte, error)
	WriteFileAtomic(path string, data []byte) error
	Remove(path string) error
	AuditAppend(logPath string, e audit.Event) (audit.Record, error)
	AuditRecords(logPath string) ([]audit.Record, error)
	AuditVerify(logPath string) (audit.VerifyResult, error)
	AuditRepairHead(logPath string) (audit.VerifyResult, error)
	AuditPruneArchivesBefore(logPath string, year int) ([]string, error)
}

// stdReviewAdminDeps is the production reviewAdminDeps backed by
// internal/review/{auth,audit}, agentslock, and fsops (mutators route through
// fsops per the FS-helpers guard).
type stdReviewAdminDeps struct{}

func (stdReviewAdminDeps) DefaultUsersPath() (string, error) { return auth.DefaultUsersPath() }
func (stdReviewAdminDeps) LoadUsers(path string) (*auth.UsersFile, error) {
	return auth.LoadUsersFile(path)
}
func (stdReviewAdminDeps) SaveUsers(uf *auth.UsersFile, path string) error { return uf.Save(path) }
func (stdReviewAdminDeps) Authenticate(usersPath, token string) (auth.Identity, error) {
	return auth.NewLocalUsersAuthenticator(usersPath).Authenticate(token)
}
func (stdReviewAdminDeps) AcquireFileLock(path string) (func() error, error) {
	return agentslock.AcquireFileLock(path)
}
func (stdReviewAdminDeps) ReadFile(path string) ([]byte, error) { return os.ReadFile(path) }
func (stdReviewAdminDeps) WriteFileAtomic(path string, data []byte) error {
	return fsops.WriteFileAtomic(path, data)
}
func (stdReviewAdminDeps) Remove(path string) error { return fsops.Remove(path) }
func (stdReviewAdminDeps) AuditAppend(logPath string, e audit.Event) (audit.Record, error) {
	return audit.Open(logPath).Append(e)
}
func (stdReviewAdminDeps) AuditRecords(logPath string) ([]audit.Record, error) {
	return audit.Open(logPath).Records()
}
func (stdReviewAdminDeps) AuditVerify(logPath string) (audit.VerifyResult, error) {
	return audit.Open(logPath).Verify()
}
func (stdReviewAdminDeps) AuditRepairHead(logPath string) (audit.VerifyResult, error) {
	return audit.Open(logPath).RepairHead()
}
func (stdReviewAdminDeps) AuditPruneArchivesBefore(logPath string, year int) ([]string, error) {
	return audit.Open(logPath).PruneArchivesBefore(year)
}

// withReviewAdmin attaches the R5 admin subcommands to the `da review` group
// built by NewReviewCmd (review.go owns the proposal surface; this file owns
// the admin surface). root.go registers the composed group.
func withReviewAdmin(review *cobra.Command) *cobra.Command {
	review.AddCommand(newReviewUsersCmd(stdReviewAdminDeps{}), newReviewAuditCmd(stdReviewAdminDeps{}))
	return review
}

// reviewAdminOpts carries the per-group persistent flag values shared by
// every review admin subcommand.
type reviewAdminOpts struct {
	usersFile string
	auditLog  string
	token     string
}

// registerFlags wires the shared persistent flags onto a subcommand group.
func (o *reviewAdminOpts) registerFlags(cmd *cobra.Command) {
	pf := cmd.PersistentFlags()
	pf.StringVar(&o.usersFile, "users-file", "", "Users file (default: ~/.config/da/review/users.yaml, honoring $XDG_CONFIG_HOME)")
	pf.StringVar(&o.auditLog, "audit-log", "", "Audit log file (default: "+defaultReviewAuditLog+")")
	pf.StringVar(&o.token, "token", "", "Admin bearer token (default: $"+reviewTokenEnv+")")
}

// resolveUsersPath returns the explicit --users-file value or the spec D5.3
// default under the per-host local-secrets home.
func (o *reviewAdminOpts) resolveUsersPath(deps reviewAdminDeps) (string, error) {
	if o.usersFile != "" {
		return o.usersFile, nil
	}
	return deps.DefaultUsersPath()
}

// resolveAuditPath returns the explicit --audit-log value or the repo-relative
// default.
func (o *reviewAdminOpts) resolveAuditPath() string {
	if o.auditLog != "" {
		return o.auditLog
	}
	return defaultReviewAuditLog
}

// resolveToken returns the explicit --token value or the env fallback.
func (o *reviewAdminOpts) resolveToken() string {
	if o.token != "" {
		return o.token
	}
	return os.Getenv(reviewTokenEnv)
}

// newReviewUsersCmd builds the `da review users` admin group.
func newReviewUsersCmd(deps reviewAdminDeps) *cobra.Command {
	opts := &reviewAdminOpts{}
	cmd := &cobra.Command{
		Use:   "users",
		Short: "Manage review RBAC users (admin only)",
		Long: "Admin-only management of the review users file (spec D5.3). Tokens are minted at\n" +
			"`users add` and printed exactly once; only the argon2id hash is persisted.\n" +
			"Every mutation writes one hash-chained audit record (spec R6) and fails closed:\n" +
			"if the audit append fails, the users-file change is rolled back.",
		Example: ExampleBlock(
			"  da review users add admin@example.com --role admin",
			"  da review users list",
			"  da review users set-role dev@example.com --role readonly",
			"  da review users remove dev@example.com",
		),
	}
	opts.registerFlags(cmd)
	cmd.AddCommand(
		newReviewUsersAddCmd(deps, opts),
		newReviewUsersListCmd(deps, opts),
		newReviewUsersRemoveCmd(deps, opts),
		newReviewUsersSetRoleCmd(deps, opts),
	)
	return cmd
}

// newReviewUsersAddCmd builds `da review users add`.
func newReviewUsersAddCmd(deps reviewAdminDeps, opts *reviewAdminOpts) *cobra.Command {
	var role string
	cmd := &cobra.Command{
		Use:   "add <email>",
		Short: "Add a user and mint their bearer token (printed once)",
		Example: ExampleBlock(
			"  da review users add admin@example.com --role admin",
			"  da review users add reviewer@example.com --role reviewer",
		),
		Args: ExactArgsWithHints(1, "Pass the new user's email address."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runReviewUsersAdd(cmd.OutOrStdout(), deps, opts, args[0], role)
		},
	}
	cmd.Flags().StringVar(&role, reviewRoleFlag, "", "Role to grant: reviewer, admin, or readonly")
	_ = cmd.MarkFlagRequired(reviewRoleFlag)
	return cmd
}

// newReviewUsersListCmd builds `da review users list`.
func newReviewUsersListCmd(deps reviewAdminDeps, opts *reviewAdminOpts) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List review users (hashed secrets only, never plaintext)",
		Example: ExampleBlock(
			"  da review users list",
			"  da review users list --json",
		),
		Args: NoArgsWithHints("`da review users list` takes no positional arguments."),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runReviewUsersList(cmd.OutOrStdout(), deps, opts)
		},
	}
}

// newReviewUsersRemoveCmd builds `da review users remove`.
func newReviewUsersRemoveCmd(deps reviewAdminDeps, opts *reviewAdminOpts) *cobra.Command {
	return &cobra.Command{
		Use:   "remove <email>",
		Short: "Remove a user (their token stops authenticating immediately)",
		Example: ExampleBlock(
			"  da review users remove dev@example.com",
		),
		Args: ExactArgsWithHints(1, "Pass the email of the user to remove."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runReviewUsersRemove(cmd.OutOrStdout(), deps, opts, args[0])
		},
	}
}

// newReviewUsersSetRoleCmd builds `da review users set-role`.
func newReviewUsersSetRoleCmd(deps reviewAdminDeps, opts *reviewAdminOpts) *cobra.Command {
	var role string
	cmd := &cobra.Command{
		Use:   "set-role <email>",
		Short: "Change a user's role (keeps their token)",
		Example: ExampleBlock(
			"  da review users set-role dev@example.com --role readonly",
		),
		Args: ExactArgsWithHints(1, "Pass the email of the user whose role should change."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runReviewUsersSetRole(cmd.OutOrStdout(), deps, opts, args[0], role)
		},
	}
	cmd.Flags().StringVar(&role, reviewRoleFlag, "", "New role: reviewer, admin, or readonly")
	_ = cmd.MarkFlagRequired(reviewRoleFlag)
	return cmd
}

// newReviewAuditCmd builds the `da review audit` group.
func newReviewAuditCmd(deps reviewAdminDeps) *cobra.Command {
	opts := &reviewAdminOpts{}
	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Inspect, verify, and compact the review audit log",
		Long: "Read, attest, and compact the append-only, hash-chained review audit log\n" +
			"(spec D5.4). `tail`, `repair`, and `prune` are admin-only; `verify` needs no\n" +
			"token (it is read-only integrity attestation, usable as a CI gate) and exits\n" +
			"non-zero on the first chain break.",
		Example: ExampleBlock(
			"  da review audit tail --limit 20",
			"  da review audit verify",
			"  da review audit repair",
			"  da review audit prune --before-year 2025",
		),
	}
	opts.registerFlags(cmd)
	cmd.AddCommand(
		newReviewAuditTailCmd(deps, opts),
		newReviewAuditVerifyCmd(deps, opts),
		newReviewAuditRepairCmd(deps, opts),
		newReviewAuditPruneCmd(deps, opts),
	)
	return cmd
}

// newReviewAuditTailCmd builds `da review audit tail` (spec appendix line 174).
// `view` is retained as a deprecated alias so anyone who scripted the earlier
// name keeps working; `tail` is the canonical recent-entries verb.
func newReviewAuditTailCmd(deps reviewAdminDeps, opts *reviewAdminOpts) *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:     "tail",
		Aliases: []string{"view"},
		Short:   "Show audit records, newest last (admin only)",
		Example: ExampleBlock(
			"  da review audit tail",
			"  da review audit tail --limit 20 --json",
		),
		Args: NoArgsWithHints("`da review audit tail` takes no positional arguments."),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runReviewAuditTail(cmd.OutOrStdout(), deps, opts, limit)
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 0, "Show only the newest N records (0 = all)")
	return cmd
}

// newReviewAuditVerifyCmd builds `da review audit verify`.
func newReviewAuditVerifyCmd(deps reviewAdminDeps, opts *reviewAdminOpts) *cobra.Command {
	return &cobra.Command{
		Use:   "verify",
		Short: "Verify the audit chain; exits non-zero on an integrity break",
		Example: ExampleBlock(
			"  da review audit verify",
			"  da review audit verify --audit-log .agents/active/review/audit.log.jsonl",
		),
		Args: NoArgsWithHints("`da review audit verify` takes no positional arguments."),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runReviewAuditVerify(cmd.OutOrStdout(), deps, opts)
		},
	}
}

// newReviewAuditRepairCmd builds `da review audit repair`.
func newReviewAuditRepairCmd(deps reviewAdminDeps, opts *reviewAdminOpts) *cobra.Command {
	return &cobra.Command{
		Use:   "repair",
		Short: "Heal a benign torn-append head anchor (admin only)",
		Long: "Re-anchors the audit head after an interrupted append left the log exactly one\n" +
			"clean, correctly-chained record ahead of its anchor (TornAppend). Any other\n" +
			"divergence is a real integrity break and is never repaired.",
		Example: ExampleBlock(
			"  da review audit repair",
		),
		Args: NoArgsWithHints("`da review audit repair` takes no positional arguments."),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runReviewAuditRepair(cmd.OutOrStdout(), deps, opts)
		},
	}
}

// newReviewAuditPruneCmd builds `da review audit prune`.
func newReviewAuditPruneCmd(deps reviewAdminDeps, opts *reviewAdminOpts) *cobra.Command {
	var beforeYear int
	cmd := &cobra.Command{
		Use:   "prune",
		Short: "Compact rotated audit archives older than a given year (admin only)",
		Long: "Removes the dated year-archive files (produced by the audit log's yearly or\n" +
			"size-cap rotation) whose year is strictly before --before-year, compacting\n" +
			"retained history (spec D5.4). The active log is never touched, and an archive\n" +
			"that fails chain verification is left in place and reported rather than deleted.\n" +
			"Prune is archive maintenance over frozen, self-contained chains, not a mutating\n" +
			"review action, so it writes no audit record.",
		Example: ExampleBlock(
			"  da review audit prune --before-year 2025",
		),
		Args: NoArgsWithHints("`da review audit prune` takes no positional arguments."),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runReviewAuditPrune(cmd.OutOrStdout(), deps, opts, beforeYear)
		},
	}
	cmd.Flags().IntVar(&beforeYear, reviewBeforeYearFlag, 0, "Remove archives whose year is strictly before this four-digit year")
	_ = cmd.MarkFlagRequired(reviewBeforeYearFlag)
	return cmd
}

// ── users runners ───────────────────────────────────────────────────────────

// reviewUserJSON is one user in JSON output. It mirrors internal/review/http's
// userJSON DTO: the stored argon2id hash is never serialized (the human table
// shows a truncated hash instead — the visible secret is always the hash,
// never the plaintext).
type reviewUserJSON struct {
	Email     string `json:"email"`
	Role      string `json:"role"`
	CreatedAt string `json:"created_at,omitempty"`
}

// reviewUserAddJSON is the `users add --json` envelope: the only place the
// plaintext token ever appears (spec OQ1 print-once).
type reviewUserAddJSON struct {
	reviewUserJSON
	Token string `json:"token"`
}

// reviewUsersListJSON is the `users list --json` envelope.
type reviewUsersListJSON struct {
	Users []reviewUserJSON `json:"users"`
}

// reviewUserRemoveJSON is the `users remove --json` envelope.
type reviewUserRemoveJSON struct {
	Email   string `json:"email"`
	Removed bool   `json:"removed"`
}

// reviewSetRoleJSON is the `users set-role --json` envelope.
type reviewSetRoleJSON struct {
	Email        string `json:"email"`
	Role         string `json:"role"`
	PreviousRole string `json:"previous_role"`
}

// runReviewUsersAdd implements `da review users add`: mint a token, persist
// the user (hash only), audit fail-closed, and print the plaintext once.
func runReviewUsersAdd(out io.Writer, deps reviewAdminDeps, opts *reviewAdminOpts, email, rawRole string) error {
	role, err := auth.ParseRole(rawRole)
	if err != nil {
		return UsageError(err.Error())
	}
	var added auth.User
	var token string
	err = runReviewUsersMutation(deps, opts, true, func(uf *auth.UsersFile, bootstrap bool) (audit.Event, error) {
		if bootstrap && role != auth.RoleAdmin {
			return audit.Event{}, ErrorWithHints(
				"the users file is empty — the first user must be an admin",
				"Bootstrap with `da review users add "+email+" --role admin`, then add reviewers using its token.",
			)
		}
		plaintext, addErr := uf.AddUser(email, role)
		if errors.Is(addErr, auth.ErrUserExists) {
			return audit.Event{}, ErrorWithHints(
				fmt.Sprintf("user %s already exists", email),
				"Use `da review users set-role` to change their role, or `remove` then `add` to rotate their token.",
			)
		}
		if addErr != nil {
			return audit.Event{}, addErr
		}
		token = plaintext
		added, _ = uf.Find(email)
		return audit.Event{
			Action:    audit.ActionUserCreate,
			Target:    reviewUserTarget(added.Email),
			AfterHash: reviewUserStateHash(added.Email, string(role), ""),
		}, nil
	})
	if err != nil {
		return err
	}
	if Flags.JSON {
		return emitReviewAdminJSON(out, reviewUserAddJSON{
			reviewUserJSON: reviewUserJSON{Email: added.Email, Role: string(added.Role), CreatedAt: added.CreatedAt},
			Token:          token,
		})
	}
	fmt.Fprintf(out, "User added: %s (%s)\n\n", added.Email, added.Role)
	fmt.Fprintf(out, "  Token (shown once — store it now): %s\n\n", token)
	fmt.Fprintln(out, "Only the argon2id hash is persisted; the plaintext cannot be recovered.")
	return nil
}

// runReviewUsersList implements `da review users list`.
func runReviewUsersList(out io.Writer, deps reviewAdminDeps, opts *reviewAdminOpts) error {
	usersPath, err := opts.resolveUsersPath(deps)
	if err != nil {
		return err
	}
	uf, err := deps.LoadUsers(usersPath)
	if err != nil {
		return err
	}
	// With zero users there is nothing to protect and no token that could
	// authenticate — short-circuit so the empty state stays discoverable.
	if len(uf.Users) == 0 {
		if Flags.JSON {
			return emitReviewAdminJSON(out, reviewUsersListJSON{Users: []reviewUserJSON{}})
		}
		fmt.Fprintf(out, "No review users in %s\n", usersPath)
		return nil
	}
	if _, err := authenticateReviewAdmin(deps, opts, usersPath, auth.PermManageUsers); err != nil {
		return err
	}
	if Flags.JSON {
		users := make([]reviewUserJSON, 0, len(uf.Users))
		for _, u := range uf.Users {
			users = append(users, reviewUserJSON{Email: u.Email, Role: string(u.Role), CreatedAt: u.CreatedAt})
		}
		return emitReviewAdminJSON(out, reviewUsersListJSON{Users: users})
	}
	fmt.Fprintf(out, fmtReviewUsersRow, "EMAIL", "ROLE", "CREATED", "TOKEN_HASH")
	for _, u := range uf.Users {
		fmt.Fprintf(out, fmtReviewUsersRow,
			truncStr(u.Email, 32), u.Role, truncStr(u.CreatedAt, 22), truncStr(u.TokenHash, 28))
	}
	fmt.Fprintf(out, "\nSource: %s\n", usersPath)
	return nil
}

// runReviewUsersRemove implements `da review users remove`.
func runReviewUsersRemove(out io.Writer, deps reviewAdminDeps, opts *reviewAdminOpts, email string) error {
	var removed auth.User
	err := runReviewUsersMutation(deps, opts, false, func(uf *auth.UsersFile, _ bool) (audit.Event, error) {
		idx := findReviewUser(uf, email)
		if idx < 0 {
			return audit.Event{}, reviewUserNotFound(email)
		}
		if wouldOrphanAdmins(uf, email) {
			return audit.Event{}, reviewLastAdminError("remove")
		}
		u := uf.Users[idx]
		uf.Users = append(uf.Users[:idx], uf.Users[idx+1:]...)
		removed = u
		return audit.Event{
			Action:     audit.ActionUserDelete,
			Target:     reviewUserTarget(u.Email),
			BeforeHash: reviewUserStateHash(u.Email, string(u.Role), u.CreatedAt),
		}, nil
	})
	if err != nil {
		return err
	}
	if Flags.JSON {
		return emitReviewAdminJSON(out, reviewUserRemoveJSON{Email: removed.Email, Removed: true})
	}
	fmt.Fprintf(out, "User removed: %s (%s)\n", removed.Email, removed.Role)
	return nil
}

// runReviewUsersSetRole implements `da review users set-role`.
func runReviewUsersSetRole(out io.Writer, deps reviewAdminDeps, opts *reviewAdminOpts, email, rawRole string) error {
	role, err := auth.ParseRole(rawRole)
	if err != nil {
		return UsageError(err.Error())
	}
	var previous auth.Role
	var changed auth.User
	err = runReviewUsersMutation(deps, opts, false, func(uf *auth.UsersFile, _ bool) (audit.Event, error) {
		idx := findReviewUser(uf, email)
		if idx < 0 {
			return audit.Event{}, reviewUserNotFound(email)
		}
		previous = uf.Users[idx].Role
		if role != auth.RoleAdmin && wouldOrphanAdmins(uf, email) {
			return audit.Event{}, reviewLastAdminError("demote")
		}
		uf.Users[idx].Role = role
		changed = uf.Users[idx]
		return audit.Event{
			Action:     audit.ActionRoleChange,
			Target:     reviewUserTarget(changed.Email),
			BeforeHash: reviewUserStateHash(changed.Email, string(previous), ""),
			AfterHash:  reviewUserStateHash(changed.Email, string(role), ""),
		}, nil
	})
	if err != nil {
		return err
	}
	if Flags.JSON {
		return emitReviewAdminJSON(out, reviewSetRoleJSON{
			Email: changed.Email, Role: string(changed.Role), PreviousRole: string(previous),
		})
	}
	fmt.Fprintf(out, "Role changed: %s %s -> %s\n", changed.Email, previous, changed.Role)
	return nil
}

// ── audit runners ───────────────────────────────────────────────────────────

// reviewAuditTailJSON is the `audit tail --json` envelope. Total is the full
// record count before --limit trimming, mirroring the HTTP audit view.
type reviewAuditTailJSON struct {
	Total   int            `json:"total"`
	Records []audit.Record `json:"records"`
}

// reviewAuditVerifyJSON is the `audit verify` / `audit repair` success
// envelope (failures stay human-first per the error-message contract).
type reviewAuditVerifyJSON struct {
	OK         bool   `json:"ok"`
	Count      int    `json:"count"`
	TornAppend bool   `json:"torn_append,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

// runReviewAuditTail implements `da review audit tail` (admin only).
func runReviewAuditTail(out io.Writer, deps reviewAdminDeps, opts *reviewAdminOpts, limit int) error {
	if limit < 0 {
		return UsageError("--limit must be a non-negative integer")
	}
	usersPath, err := opts.resolveUsersPath(deps)
	if err != nil {
		return err
	}
	if _, err := authenticateReviewAdmin(deps, opts, usersPath, auth.PermReadAudit); err != nil {
		return err
	}
	logPath := opts.resolveAuditPath()
	recs, err := deps.AuditRecords(logPath)
	if err != nil {
		return err
	}
	total := len(recs)
	if limit > 0 && limit < total {
		recs = recs[total-limit:]
	}
	if Flags.JSON {
		return emitReviewAdminJSON(out, reviewAuditTailJSON{Total: total, Records: recs})
	}
	if total == 0 {
		fmt.Fprintf(out, "No audit records in %s\n", logPath)
		return nil
	}
	fmt.Fprintf(out, fmtReviewAuditRow, "TS", "ACTOR", "ROLE", "ACTION", "TARGET")
	for _, r := range recs {
		fmt.Fprintf(out, fmtReviewAuditRow,
			r.Ts.UTC().Format(time.RFC3339), truncStr(r.Actor, 24), truncStr(r.Role, 9),
			truncStr(string(r.Action), 13), r.Target)
	}
	fmt.Fprintf(out, "\nShowing %d of %d record(s) — %s\n", len(recs), total, logPath)
	return nil
}

// runReviewAuditVerify implements `da review audit verify`. It requires no
// token: verification is read-only integrity attestation (spec R7) and a
// candidate CI gate, where no reviewer token exists.
func runReviewAuditVerify(out io.Writer, deps reviewAdminDeps, opts *reviewAdminOpts) error {
	logPath := opts.resolveAuditPath()
	res, err := deps.AuditVerify(logPath)
	if err != nil {
		return err
	}
	if !res.OK {
		return reviewAuditBroken(logPath, res)
	}
	if Flags.JSON {
		return emitReviewAdminJSON(out, reviewAuditVerifyJSON{
			OK: true, Count: res.Count, TornAppend: res.TornAppend, Reason: res.Reason,
		})
	}
	fmt.Fprintf(out, "audit chain OK — %d record(s) in %s\n", res.Count, logPath)
	if res.TornAppend {
		fmt.Fprintf(out, "\nwarning: torn append — %s\n", res.Reason)
		fmt.Fprintln(out, "Run `da review audit repair` after reviewing the tail record.")
	}
	return nil
}

// runReviewAuditRepair implements `da review audit repair` (admin only — it
// mutates the head anchor, though never the record lines themselves).
func runReviewAuditRepair(out io.Writer, deps reviewAdminDeps, opts *reviewAdminOpts) error {
	usersPath, err := opts.resolveUsersPath(deps)
	if err != nil {
		return err
	}
	if _, err := authenticateReviewAdmin(deps, opts, usersPath, auth.PermReadAudit); err != nil {
		return err
	}
	logPath := opts.resolveAuditPath()
	res, err := deps.AuditRepairHead(logPath)
	if err != nil {
		return err
	}
	if !res.OK {
		return reviewAuditBroken(logPath, res)
	}
	if Flags.JSON {
		return emitReviewAdminJSON(out, reviewAuditVerifyJSON{OK: true, Count: res.Count})
	}
	fmt.Fprintf(out, "audit head anchor is consistent — %d record(s) in %s\n", res.Count, logPath)
	return nil
}

// reviewAuditPruneJSON is the `audit prune --json` envelope. Removed lists the
// archive files compacted (never null — an empty prune renders []).
type reviewAuditPruneJSON struct {
	BeforeYear int      `json:"before_year"`
	Count      int      `json:"count"`
	Removed    []string `json:"removed"`
}

// runReviewAuditPrune implements `da review audit prune` (admin only). It
// compacts rotated year-archives older than --before-year through the audit
// package's PruneArchivesBefore primitive. Prune is archive maintenance, not an
// R6 mutation (spec line 111): the removed archives are frozen, self-contained
// chains and the active log is untouched, so there is deliberately NO
// fail-closed audit record here (contrast the users-file mutators).
func runReviewAuditPrune(out io.Writer, deps reviewAdminDeps, opts *reviewAdminOpts, beforeYear int) error {
	if beforeYear < 1000 || beforeYear > 9999 {
		return UsageError("--before-year must be a four-digit year (1000-9999)")
	}
	usersPath, err := opts.resolveUsersPath(deps)
	if err != nil {
		return err
	}
	if _, err := authenticateReviewAdmin(deps, opts, usersPath, auth.PermReadAudit); err != nil {
		return err
	}
	logPath := opts.resolveAuditPath()
	removed, err := deps.AuditPruneArchivesBefore(logPath, beforeYear)
	if err != nil {
		return reviewPruneFailed(removed, err)
	}
	return emitReviewPrune(out, logPath, beforeYear, removed)
}

// reviewPruneFailed renders a prune that hit a corrupt or unreadable archive.
// removed still names the archives that WERE compacted before the failure, so
// a re-run resumes safely.
func reviewPruneFailed(removed []string, cause error) error {
	return ErrorWithHints(
		fmt.Sprintf("audit prune incomplete (%d archive(s) compacted): %v", len(removed), cause),
		"Investigate the named archive(s); intact older archives were still compacted, so a re-run resumes safely.",
	)
}

// emitReviewPrune renders a successful prune (human table or --json).
func emitReviewPrune(out io.Writer, logPath string, beforeYear int, removed []string) error {
	if Flags.JSON {
		return emitReviewAdminJSON(out, reviewAuditPruneJSON{BeforeYear: beforeYear, Count: len(removed), Removed: removed})
	}
	if len(removed) == 0 {
		fmt.Fprintf(out, "No audit archives older than %d to compact — %s\n", beforeYear, logPath)
		return nil
	}
	fmt.Fprintf(out, "Compacted %d audit archive(s) older than %d:\n", len(removed), beforeYear)
	for _, p := range removed {
		fmt.Fprintf(out, "  %s\n", p)
	}
	return nil
}

// reviewAuditBroken renders an integrity break as a non-zero-exit error naming
// the first broken record (spec R7). Repair never fixes this state.
func reviewAuditBroken(logPath string, res audit.VerifyResult) error {
	return ErrorWithHints(
		fmt.Sprintf("audit chain integrity break at record %d: %s", res.BrokenAt, res.Reason),
		"The log at "+logPath+" diverged from its hash chain; investigate before trusting labels or user changes.",
	)
}

// ── shared mutation pipeline ────────────────────────────────────────────────

// reviewUserMutation applies one users-file change in place and returns the
// audit event describing it (actor, role, and request id are stamped by
// runReviewUsersMutation). bootstrap reports whether the file was empty when
// the mutation ran.
type reviewUserMutation func(uf *auth.UsersFile, bootstrap bool) (audit.Event, error)

// runReviewUsersMutation is the audited users-file mutation pipeline shared by
// add/remove/set-role. It serializes the whole [load → mutate → save → audit]
// section under the agentslock file lock on the users file (the same lock the
// review HTTP handlers take for the same file, so CLI and service processes
// cannot interleave read-modify-write cycles), captures the pre-image, and
// enforces the fail-closed audit invariant (spec R6): a failed audit append
// rolls the users file back and fails the command.
func runReviewUsersMutation(deps reviewAdminDeps, opts *reviewAdminOpts, allowBootstrap bool, apply reviewUserMutation) error {
	usersPath, err := opts.resolveUsersPath(deps)
	if err != nil {
		return err
	}
	release, err := deps.AcquireFileLock(usersPath)
	if err != nil {
		return fmt.Errorf("lock users file %s: %w", usersPath, err)
	}
	defer func() { _ = release() }()

	uf, err := deps.LoadUsers(usersPath)
	if err != nil {
		return err
	}
	bootstrap := len(uf.Users) == 0
	id, err := reviewAdminIdentity(deps, opts, usersPath, bootstrap && allowBootstrap)
	if err != nil {
		return err
	}
	pre, err := readReviewPreImage(deps, usersPath)
	if err != nil {
		return fmt.Errorf("capture users-file pre-image: %w", err)
	}
	ev, err := apply(uf, bootstrap)
	if err != nil {
		return err
	}
	if err := deps.SaveUsers(uf, usersPath); err != nil {
		return err
	}
	ev.Actor = id.Email
	ev.Role = string(id.Role)
	ev.RequestID = cliRequestID()
	if _, err := deps.AuditAppend(opts.resolveAuditPath(), ev); err != nil {
		return failClosedReviewMutation(deps, usersPath, pre, ev.RequestID, err)
	}
	return nil
}

// reviewAdminIdentity resolves the acting identity for a mutating command.
// When the users file is empty and the command allows bootstrap, the mutation
// runs as the synthetic bootstrap actor — there is no admin yet to
// authenticate, and the operator already holds filesystem access to the users
// file. Otherwise the presented token must resolve to a user holding the
// admin user-management permission (spec R9).
func reviewAdminIdentity(deps reviewAdminDeps, opts *reviewAdminOpts, usersPath string, bootstrap bool) (auth.Identity, error) {
	if bootstrap {
		return auth.Identity{Email: reviewBootstrapActor, Role: auth.RoleAdmin}, nil
	}
	return authenticateReviewAdmin(deps, opts, usersPath, auth.PermManageUsers)
}

// authenticateReviewAdmin resolves the bearer token and gates on perm,
// mirroring the HTTP layer's 401/403 split as CLI errors (spec R8/R9).
func authenticateReviewAdmin(deps reviewAdminDeps, opts *reviewAdminOpts, usersPath string, perm auth.Permission) (auth.Identity, error) {
	token := opts.resolveToken()
	if token == "" {
		return auth.Identity{}, ErrorWithHints(
			"a review admin token is required",
			"Set "+reviewTokenEnv+" or pass --token.",
			"If no users exist yet, bootstrap with `da review users add <email> --role admin`.",
		)
	}
	id, err := deps.Authenticate(usersPath, token)
	if errors.Is(err, auth.ErrUnauthenticated) {
		return auth.Identity{}, ErrorWithHints(
			"invalid review token",
			"Tokens are minted by `da review users add` and shown exactly once; ask an admin to re-issue yours.",
		)
	}
	if err != nil {
		return auth.Identity{}, err
	}
	if !id.Can(perm) {
		return auth.Identity{}, ErrorWithHints(
			fmt.Sprintf("role %s lacks permission %s — this command is admin-only", id.Role, perm),
		)
	}
	return id, nil
}

// reviewPreImage is the users file's byte-exact state before a mutation, used
// for the fail-closed rollback (mirrors internal/review/http's preImage).
type reviewPreImage struct {
	data    []byte
	existed bool
}

// readReviewPreImage snapshots the users file; a missing file is a valid
// pre-image (rollback then removes whatever the mutation created).
func readReviewPreImage(deps reviewAdminDeps, path string) (reviewPreImage, error) {
	data, err := deps.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return reviewPreImage{}, nil
	}
	if err != nil {
		return reviewPreImage{}, err
	}
	return reviewPreImage{data: data, existed: true}, nil
}

// failClosedReviewMutation enforces the no-unaudited-mutation invariant after
// a failed audit append (spec R6, mirroring internal/review/http's guard):
// the users file is restored to its pre-image while the file lock is still
// held. If the rollback itself also fails, the mutation persisted UNAUDITED —
// stated loudly with the request id so the operator can reconcile against any
// audit line that did land (audit.Append is at-least-once; never blind-retry).
func failClosedReviewMutation(deps reviewAdminDeps, path string, pre reviewPreImage, reqID string, cause error) error {
	var rbErr error
	if pre.existed {
		rbErr = deps.WriteFileAtomic(path, pre.data)
	} else if rbErr = deps.Remove(path); errors.Is(rbErr, os.ErrNotExist) {
		// The mutation never materialized the file; absence IS the pre-image.
		rbErr = nil
	}
	if rbErr == nil {
		return ErrorWithHints(
			fmt.Sprintf("audit append failed; the users-file change was rolled back (request_id=%s): %v", reqID, cause),
			"Fix the audit log, then re-run — but check `da review audit view` for a record with this request id first (audit.Append is at-least-once).",
		)
	}
	return ErrorWithHints(
		fmt.Sprintf("CRITICAL: audit append failed AND rollback failed — the users-file change persisted UNAUDITED (request_id=%s): audit: %v; rollback: %v", reqID, cause, rbErr),
		"Reconcile "+path+" against the audit log manually before trusting either.",
	)
}

// ── small helpers ───────────────────────────────────────────────────────────

// reviewUserTarget builds the audit-record target for a user mutation,
// matching the shape internal/review/http writes for the same actions.
func reviewUserTarget(email string) string { return "user/" + email }

// reviewUserStateHash is the hex SHA-256 of the {email, role[, created_at]}
// JSON DTO — the same shape internal/review/http hashes into before/after
// audit fields, so CLI- and HTTP-written records for the same user state hash
// identically. The DTO is a plain struct; json.Marshal cannot fail on it.
func reviewUserStateHash(email, role, createdAt string) string {
	data, _ := json.Marshal(reviewUserJSON{Email: email, Role: role, CreatedAt: createdAt})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// findReviewUser returns the index of the user with the given email
// (case-insensitive, whitespace-trimmed), or -1.
func findReviewUser(uf *auth.UsersFile, email string) int {
	want := strings.ToLower(strings.TrimSpace(email))
	for i, u := range uf.Users {
		if strings.ToLower(strings.TrimSpace(u.Email)) == want {
			return i
		}
	}
	return -1
}

// reviewUserNotFound renders the shared user-lookup failure.
func reviewUserNotFound(email string) error {
	return ErrorWithHints(
		fmt.Sprintf("user not found: %s", email),
		"Run `da review users list` to see known users.",
	)
}

// wouldOrphanAdmins reports whether stripping admin rights from email — by
// deleting the user or demoting them out of the admin role — would leave the
// users file with no admin at all. Because bootstrap only fires on an EMPTY
// users file, a non-empty file with zero admins has no CLI recovery path, so
// both mutators refuse such an operation. It MUST run inside the users-file lock
// so the admin count is consistent with the write that follows.
func wouldOrphanAdmins(uf *auth.UsersFile, email string) bool {
	target, ok := uf.Find(email)
	if !ok || target.Role != auth.RoleAdmin {
		return false
	}
	admins := 0
	for _, u := range uf.Users {
		if u.Role == auth.RoleAdmin {
			admins++
		}
	}
	return admins == 1
}

// reviewLastAdminError renders the last-admin lockout guard failure; action is
// the attempted verb ("remove" or "demote").
func reviewLastAdminError(action string) error {
	return ErrorWithHints(
		"refusing to "+action+" the last admin — the users file would have no admin and no CLI recovery path",
		"Promote another user to admin first (`da review users set-role <email> --role admin`), then retry.",
	)
}

// cliRequestID builds the audit idempotency key for one CLI mutation. It is
// deliberately non-cryptographic — pid+nanotime is unique enough to reconcile
// a failed run against any audit line it may have landed, with no error
// branch to cover (contrast the HTTP layer, which honors client-supplied ids).
func cliRequestID() string {
	return fmt.Sprintf("cli-%d-%d", os.Getpid(), time.Now().UnixNano())
}

// emitReviewAdminJSON renders v as indented JSON, matching the score.go
// emitter conventions.
func emitReviewAdminJSON(out io.Writer, v any) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
