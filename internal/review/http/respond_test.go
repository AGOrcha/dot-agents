package http

import (
	"errors"
	nethttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestWriteJSONMarshalFailure covers the defensive encode branch via the
// marshal seam.
func TestWriteJSONMarshalFailure(t *testing.T) {
	orig := marshalJSON
	marshalJSON = func(any) ([]byte, error) { return nil, errors.New("boom") }
	defer func() { marshalJSON = orig }()

	rr := httptest.NewRecorder()
	writeJSON(rr, nethttp.StatusOK, struct{}{})
	if rr.Code != nethttp.StatusInternalServerError {
		t.Fatalf("status = %d", rr.Code)
	}

	if got := hashJSON(struct{}{}); got != "" {
		t.Fatalf("hashJSON under marshal failure = %q, want empty", got)
	}
}

// TestHashJSON pins determinism and shape of the audit content hash.
func TestHashJSON(t *testing.T) {
	a := hashJSON(userJSON{Email: "a@b", Role: "reviewer"})
	b := hashJSON(userJSON{Email: "a@b", Role: "reviewer"})
	c := hashJSON(userJSON{Email: "a@b", Role: "admin"})
	if a == "" || a != b || a == c || len(a) != 64 {
		t.Fatalf("hashJSON: a=%q b=%q c=%q", a, b, c)
	}
}

// TestWriteErrorShape pins the uniform error body.
func TestWriteErrorShape(t *testing.T) {
	rr := httptest.NewRecorder()
	writeError(rr, nethttp.StatusBadRequest, "bad thing", "req-1")
	if rr.Header().Get("Content-Type") != contentTypeJSON {
		t.Fatalf("content type = %q", rr.Header().Get("Content-Type"))
	}
	var body errorBody
	decodeBody(t, rr, &body)
	if body.Error != "bad thing" || body.RequestID != "req-1" {
		t.Fatalf("error body: %+v", body)
	}
}

// TestDecodeJSONOversizedBody confirms the request-size cap yields a 400.
func TestDecodeJSONOversizedBody(t *testing.T) {
	huge := `{"free_text":"` + strings.Repeat("x", maxBodyBytes+1) + `"}`
	req := httptest.NewRequest(nethttp.MethodPost, "/x", strings.NewReader(huge))
	rr := httptest.NewRecorder()
	var v struct {
		FreeText string `json:"free_text"`
	}
	if decodeJSON(rr, req, &v) {
		t.Fatal("oversized body should fail decode")
	}
	if rr.Code != nethttp.StatusBadRequest {
		t.Fatalf("status = %d", rr.Code)
	}
}
