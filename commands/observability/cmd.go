// Package observability implements the `da observability` command subtree.
package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	cfg "github.com/AGOrcha/dot-agents/internal/config"
	"github.com/AGOrcha/dot-agents/internal/credstore"
	"github.com/spf13/cobra"
)

const (
	healthPath = "/api/v1/observability/health"
	ingestPath = "/api/v1/observability/ingest"
)

type credentialResolver interface {
	Resolve(id string) (string, error)
}

type credentialStore interface {
	Set(id, secret string)
	Save() error
}

type httpDoer interface {
	Do(*http.Request) (*http.Response, error)
}

// Deps carries build metadata and global output flags from package commands.
// Runtime collaborators have production defaults and are replaced only by
// same-package tests.
type Deps struct {
	Version string
	JSON    func() bool

	getwd       func() (string, error)
	loadConfig  func(string) (*cfg.AgentsRC, error)
	newResolver func() credentialResolver
	openStore   func() (credentialStore, error)
	httpClient  httpDoer
	now         func() time.Time
}

func (d Deps) withDefaults() Deps {
	if d.Version == "" {
		d.Version = "dev"
	}
	if d.getwd == nil {
		d.getwd = os.Getwd
	}
	if d.loadConfig == nil {
		d.loadConfig = cfg.LoadAgentsRC
	}
	if d.newResolver == nil {
		d.newResolver = func() credentialResolver { return credstore.NewLoader() }
	}
	if d.openStore == nil {
		d.openStore = openCredentialStore
	}
	if d.httpClient == nil {
		d.httpClient = &http.Client{
			Timeout: 15 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}
	if d.now == nil {
		d.now = time.Now
	}
	return d
}

func (d Deps) jsonOutput() bool { return d.JSON != nil && d.JSON() }

func openCredentialStore() (credentialStore, error) {
	path, err := credstore.DefaultPath()
	if err != nil {
		return nil, err
	}
	ring := credstore.NewOSKeyring()
	if ring == nil {
		return nil, errors.New("encrypted credential storage is unavailable on this platform")
	}
	return credstore.Open(path, ring)
}

// NewCmd builds the `da observability login|sync|status` subtree.
func NewCmd(deps Deps) *cobra.Command {
	deps = deps.withDefaults()
	cmd := &cobra.Command{
		Use:   "observability",
		Short: "Publish and inspect workflow observability",
	}
	cmd.AddCommand(newLoginCmd(deps), newSyncCmd(deps), newStatusCmd(deps))
	return cmd
}

func newLoginCmd(deps Deps) *cobra.Command {
	var fromEnv bool
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Store the observability service-token credential",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !fromEnv {
				return errors.New("observability login currently requires --from-env")
			}
			cwd, err := deps.getwd()
			if err != nil {
				return fmt.Errorf("resolve current directory: %w", err)
			}
			return runLogin(cmd.OutOrStdout(), cwd, deps)
		},
	}
	cmd.Flags().BoolVar(&fromEnv, "from-env", false, "Read CF_OBS_CLIENT_ID and CF_OBS_CLIENT_SECRET")
	return cmd
}

func runLogin(out io.Writer, projectDir string, deps Deps) error {
	rc, err := deps.loadConfig(projectDir)
	if err != nil {
		return fmt.Errorf("load .agentsrc.json: %w", err)
	}
	obs, err := requireObservability(rc)
	if err != nil {
		return err
	}
	if obs.Auth == nil {
		return errors.New("observability auth credential reference is not configured")
	}
	clientID := strings.TrimSpace(os.Getenv("CF_OBS_CLIENT_ID"))
	clientSecret := strings.TrimSpace(os.Getenv("CF_OBS_CLIENT_SECRET"))
	if clientID == "" || clientSecret == "" {
		return errors.New("CF_OBS_CLIENT_ID and CF_OBS_CLIENT_SECRET must both be non-empty")
	}
	serialized, err := json.Marshal(serviceTokenCredential{
		Kind:         serviceTokenKind,
		ClientID:     clientID,
		ClientSecret: clientSecret,
	})
	if err != nil {
		return fmt.Errorf("serialize observability credential: %w", err)
	}
	store, err := deps.openStore()
	if err != nil {
		return fmt.Errorf("open credential store: %w", err)
	}
	store.Set(obs.Auth.ID, string(serialized))
	if err := store.Save(); err != nil {
		return fmt.Errorf("save credential store: %w", err)
	}
	_, err = fmt.Fprintf(out, "stored observability credential %q\n", obs.Auth.ID)
	return err
}

func newStatusCmd(deps Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Check observability reachability and authentication",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cwd, err := deps.getwd()
			if err != nil {
				return fmt.Errorf("resolve current directory: %w", err)
			}
			return runStatus(cmd.Context(), cmd.OutOrStdout(), cwd, deps)
		},
	}
}

type statusResult struct {
	Endpoint   string `json:"endpoint"`
	Reachable  bool   `json:"reachable"`
	Authed     bool   `json:"authed"`
	StatusCode int    `json:"status_code,omitempty"`
	Message    string `json:"message,omitempty"`
}

func runStatus(ctx context.Context, out io.Writer, projectDir string, deps Deps) error {
	rc, err := deps.loadConfig(projectDir)
	if err != nil {
		return fmt.Errorf("load .agentsrc.json: %w", err)
	}
	obs, err := requireObservability(rc)
	if err != nil {
		return err
	}
	headers, endpoint, err := authorization(obs, deps.newResolver(), os.Getenv("DA_OBS_TEST_JWT"))
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL(endpoint, healthPath), nil)
	if err != nil {
		return fmt.Errorf("build observability health request: %w", err)
	}
	applyHeaders(req, headers)
	res, err := deps.httpClient.Do(req)
	if err != nil {
		result := statusResult{Endpoint: endpoint.String(), Message: sanitizeError(err.Error())}
		_ = writeStatus(out, result, deps.jsonOutput())
		return fmt.Errorf("observability endpoint is unreachable: %w", err)
	}
	defer res.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	if readErr != nil {
		return fmt.Errorf("read observability health response: %w", readErr)
	}
	result := statusResult{
		Endpoint:   endpoint.String(),
		Reachable:  true,
		Authed:     res.StatusCode != http.StatusUnauthorized && res.StatusCode != http.StatusForbidden,
		StatusCode: res.StatusCode,
		Message:    responseMessage(body),
	}
	if err := writeStatus(out, result, deps.jsonOutput()); err != nil {
		return err
	}
	if !result.Authed {
		return fmt.Errorf("observability authentication failed: HTTP %d%s", res.StatusCode, suffixMessage(result.Message))
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		// The o4 scaffold deliberately exposes the authenticated health seam as
		// 501 until the D1 read model lands; it still proves reachability/auth.
		if res.StatusCode != http.StatusNotImplemented {
			return fmt.Errorf("observability health check failed: HTTP %d%s", res.StatusCode, suffixMessage(result.Message))
		}
	}
	return nil
}

func writeStatus(out io.Writer, result statusResult, jsonOut bool) error {
	if jsonOut {
		enc := json.NewEncoder(out)
		enc.SetEscapeHTML(false)
		return enc.Encode(result)
	}
	yesNo := func(v bool) string {
		if v {
			return "yes"
		}
		return "no"
	}
	if _, err := fmt.Fprintf(out, "reachable: %s\nauthed: %s\n", yesNo(result.Reachable), yesNo(result.Authed)); err != nil {
		return err
	}
	if result.StatusCode != 0 {
		_, err := fmt.Fprintf(out, "health: HTTP %d%s\n", result.StatusCode, suffixMessage(result.Message))
		return err
	}
	if result.Message != "" {
		_, err := fmt.Fprintf(out, "failure: %s\n", result.Message)
		return err
	}
	return nil
}

func newSyncCmd(deps Deps) *cobra.Command {
	var full bool
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Drain the observability outbox",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cwd, err := deps.getwd()
			if err != nil {
				return fmt.Errorf("resolve current directory: %w", err)
			}
			report, syncErr := syncProject(cmd.Context(), cwd, deps, syncOptions{Explicit: true, Full: full})
			if err := writeSyncReport(cmd.OutOrStdout(), report, deps.jsonOutput()); err != nil {
				return err
			}
			return syncErr
		},
	}
	cmd.Flags().BoolVar(&full, "full", false, "Replay canonical local history after draining the outbox")
	return cmd
}

// PublishBestEffort drains due outbox entries for workflow-hook callers. It
// deliberately returns only a report: publication can never replace the local
// workflow command's exit status. Explicit `da observability sync` uses the
// same state machine with forced attempts and returns failures as errors.
func PublishBestEffort(ctx context.Context, projectDir, version string, out io.Writer) SyncReport {
	deps := (Deps{Version: version}).withDefaults()
	report, _ := syncProject(ctx, projectDir, deps, syncOptions{})
	if out != nil {
		_ = writeSyncReport(out, report, false)
	}
	return report
}

func writeSyncReport(out io.Writer, report SyncReport, jsonOut bool) error {
	if jsonOut {
		enc := json.NewEncoder(out)
		enc.SetEscapeHTML(false)
		return enc.Encode(report)
	}
	_, err := fmt.Fprintf(out, "observability sync: accepted=%d deduped=%d retained=%d quarantined=%d\n",
		report.Accepted, report.Deduped, report.Retained, report.Quarantined)
	return err
}

func requireObservability(rc *cfg.AgentsRC) (*cfg.AgentsRCObservability, error) {
	if rc == nil || rc.Observability == nil {
		return nil, errors.New("observability is not configured in .agentsrc.json")
	}
	if !rc.Observability.Enabled {
		return nil, errors.New("observability is disabled in .agentsrc.json")
	}
	if strings.TrimSpace(rc.Observability.Endpoint) == "" {
		return nil, errors.New("observability endpoint is empty")
	}
	return rc.Observability, nil
}

const serviceTokenKind = "cf-access-service-token"

type serviceTokenCredential struct {
	Kind         string `json:"kind"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

func authorization(obs *cfg.AgentsRCObservability, resolver credentialResolver, fixtureJWT string) (http.Header, *url.URL, error) {
	endpoint, err := parseEndpoint(obs.Endpoint)
	if err != nil {
		return nil, nil, err
	}
	loopback := isLoopback(endpoint.Hostname())
	if loopback && strings.TrimSpace(fixtureJWT) != "" {
		headers := make(http.Header)
		headers.Set("Cf-Access-Jwt-Assertion", strings.TrimSpace(fixtureJWT))
		return headers, endpoint, nil
	}

	// The transport guard intentionally precedes every auth/ref check and every
	// resolver call. A credential is never looked up for an HTTP endpoint.
	if !strings.EqualFold(endpoint.Scheme, "https") {
		if loopback {
			return nil, nil, errors.New("loopback HTTP observability requires DA_OBS_TEST_JWT")
		}
		return nil, nil, errors.New("observability credential-ref endpoint must be an absolute https URL")
	}
	if obs.Auth == nil || strings.TrimSpace(obs.Auth.ID) == "" {
		return nil, nil, errors.New("observability auth credential reference is not configured")
	}
	if resolver == nil {
		return nil, nil, errors.New("observability credential resolver is unavailable")
	}
	serialized, err := resolver.Resolve(obs.Auth.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve observability credential %q: %w", obs.Auth.ID, err)
	}
	credential, err := parseServiceTokenCredential(serialized)
	if err != nil {
		return nil, nil, fmt.Errorf("credential %q: %w", obs.Auth.ID, err)
	}
	headers := make(http.Header)
	headers.Set("Cf-Access-Client-Id", credential.ClientID)
	headers.Set("Cf-Access-Client-Secret", credential.ClientSecret)
	return headers, endpoint, nil
}

func parseEndpoint(raw string) (*url.URL, error) {
	endpoint, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !endpoint.IsAbs() || endpoint.Host == "" {
		return nil, errors.New("observability endpoint must be an absolute URL")
	}
	if endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return nil, errors.New("observability endpoint must not contain user info, query, or fragment")
	}
	if !strings.EqualFold(endpoint.Scheme, "https") && !strings.EqualFold(endpoint.Scheme, "http") {
		return nil, errors.New("observability endpoint scheme must be https (or loopback http for fixture mode)")
	}
	return endpoint, nil
}

func isLoopback(host string) bool {
	return strings.EqualFold(host, "localhost") || host == "127.0.0.1" || host == "::1"
}

func parseServiceTokenCredential(serialized string) (serviceTokenCredential, error) {
	var credential serviceTokenCredential
	decoder := json.NewDecoder(strings.NewReader(serialized))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&credential); err != nil {
		return credential, fmt.Errorf("invalid service-token JSON: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return credential, err
	}
	if credential.Kind != serviceTokenKind {
		return credential, fmt.Errorf("kind must be %q", serviceTokenKind)
	}
	if strings.TrimSpace(credential.ClientID) == "" || strings.TrimSpace(credential.ClientSecret) == "" {
		return credential, errors.New("client_id and client_secret must both be non-empty")
	}
	return credential, nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("JSON contains trailing data")
		}
		return fmt.Errorf("invalid trailing JSON: %w", err)
	}
	return nil
}

func apiURL(endpoint *url.URL, route string) string {
	copy := *endpoint
	copy.Path = strings.TrimRight(copy.Path, "/") + route
	copy.RawPath = ""
	return copy.String()
}

func applyHeaders(req *http.Request, headers http.Header) {
	for key, values := range headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
}

func responseMessage(body []byte) string {
	var envelope struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &envelope) == nil && envelope.Error.Message != "" {
		return sanitizeError(envelope.Error.Message)
	}
	return sanitizeError(string(bytes.TrimSpace(body)))
}

func suffixMessage(message string) string {
	if message == "" {
		return ""
	}
	return " (" + message + ")"
}

func sanitizeError(message string) string {
	message = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, message)
	message = strings.Join(strings.Fields(message), " ")
	if len(message) > 512 {
		message = message[:512]
	}
	return message
}

func outboxDir(projectDir string) string {
	return filepath.Join(projectDir, ".agents", "active", "obs-outbox")
}
