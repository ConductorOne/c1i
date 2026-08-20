package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestPoliciesValidateCelReturnsUsageErrorAndPrintsMarkers pins the exit-2
// contract: a non-empty "markers" array must produce an error errors.As
// identifies as *usageError, and the marker report must still have been
// written to stdout before that error is returned. Printing markers and
// then silently swallowing them behind an error exit would defeat the
// point of a pre-flight check whose only value is the report.
func TestPoliciesValidateCelReturnsUsageErrorAndPrintsMarkers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"markers":[{"severity":"ERROR","message":"undeclared reference to 'nope'"}]}`)
	}))
	defer srv.Close()

	stubPoliciesClient(t, srv)
	t.Setenv("C1I_URL", srv.URL)

	var out bytes.Buffer
	policiesValidateCelCmd.SetOut(&out)
	policiesValidateCelCmd.SetContext(context.Background())

	err := policiesValidateCelCmd.RunE(policiesValidateCelCmd, []string{"nope.field"})

	var usageErr *usageError
	if !errors.As(err, &usageErr) {
		t.Fatalf("RunE err = %v (%T), want *usageError", err, err)
	}

	if !bytesContains(out.String(), `"markers"`) {
		t.Errorf("expected the marker report to be written before the error, got: %s", out.String())
	}
	if !bytesContains(out.String(), "undeclared reference to 'nope'") {
		t.Errorf("expected the compile-error message in the report, got: %s", out.String())
	}
}

// TestPoliciesValidateCelEmptyMarkersIsValid is the negative half of the
// exit-2 pair: an empty "markers" array must return nil and report valid.
func TestPoliciesValidateCelEmptyMarkersIsValid(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"markers":[]}`)
	}))
	defer srv.Close()

	stubPoliciesClient(t, srv)
	t.Setenv("C1I_URL", srv.URL)

	var out bytes.Buffer
	policiesValidateCelCmd.SetOut(&out)
	policiesValidateCelCmd.SetContext(context.Background())

	if err := policiesValidateCelCmd.RunE(policiesValidateCelCmd, []string{"true"}); err != nil {
		t.Fatalf("RunE: %v, want nil for an empty markers array", err)
	}
	if !bytesContains(out.String(), `"valid": true`) {
		t.Errorf(`expected "valid": true in output, got: %s`, out.String())
	}
}

// TestPoliciesValidateCelNullMarkersIsValid guards the json-null-to-nil-slice
// case: the API can send "markers": null (not "[]") for a valid condition.
// A naive `len(resp.Markers) == 0` check happens to handle this correctly,
// but a refactor that instead tests resp.Markers != nil would silently
// invert the outcome — this must behave identically to the empty-array case.
func TestPoliciesValidateCelNullMarkersIsValid(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"markers":null}`)
	}))
	defer srv.Close()

	stubPoliciesClient(t, srv)
	t.Setenv("C1I_URL", srv.URL)

	var out bytes.Buffer
	policiesValidateCelCmd.SetOut(&out)
	policiesValidateCelCmd.SetContext(context.Background())

	if err := policiesValidateCelCmd.RunE(policiesValidateCelCmd, []string{"true"}); err != nil {
		t.Fatalf("RunE: %v, want nil for a null markers field", err)
	}
	if !bytesContains(out.String(), `"valid": true`) {
		t.Errorf(`expected "valid": true in output, got: %s`, out.String())
	}
}
