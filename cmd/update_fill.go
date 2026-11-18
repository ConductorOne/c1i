package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ConductorOne/c1i/internal/client"
)

// The server reads a field's camelCase and snake_case spellings as one field,
// so a body key is matched to an API field name the way mask paths are
// (foldMaskName). Matching exact camelCase would fill a field the body already
// sets as snake_case and send it twice (a 400, "duplicate field").

// bodySetsField reports whether obj sets field in any spelling.
func bodySetsField(obj map[string]any, field string) bool {
	want := foldMaskName(field)
	for k := range obj {
		if foldMaskName(k) == want {
			return true
		}
	}
	return false
}

// requiredFieldsFill copies each of fields from the object a GET of path holds
// under envelope into obj, where obj leaves it unset: the server rejects an
// update without them, even when the mask names none. label names the object
// in errors ("template"). The read also makes an unknown id fail before the
// send, --dry-run included.
func requiredFieldsFill(path, envelope, label string, fields []string, obj map[string]any) preRead {
	return preRead{Run: func(ctx context.Context, c *client.Client, _ *mutation) ([]resolvedID, error) {
		var missing []string
		for _, f := range fields {
			if !bodySetsField(obj, f) {
				missing = append(missing, f)
			}
		}
		if len(missing) == 0 {
			return nil, nil
		}
		data, err := c.Get(ctx, path, nil)
		if err != nil {
			return nil, fmt.Errorf("reading the %s before the update: %w", label, err)
		}
		var resp map[string]json.RawMessage
		var cur map[string]any
		if json.Unmarshal(data, &resp) != nil || json.Unmarshal(resp[envelope], &cur) != nil || cur == nil {
			return nil, &nonJSONResponseError{fmt.Errorf("reading the %s before the update: the response held no %s", label, envelope)}
		}
		for _, f := range missing {
			v, ok := cur[f].(string)
			if !ok || v == "" {
				return nil, &nonJSONResponseError{fmt.Errorf("reading the %s before the update: the response held no %s", label, f)}
			}
			obj[f] = v
		}
		return nil, nil
	}}
}

// refuseMaskedFieldWithoutValue refuses a mask that names one of fields the
// body does not set: requiredFieldsFill would send the current value, a silent
// no-op.
func refuseMaskedFieldWithoutValue(mask string, obj map[string]any, fields []string) error {
	for _, path := range strings.Split(mask, ",") {
		for _, f := range fields {
			if foldMaskName(path) == foldMaskName(f) && !bodySetsField(obj, f) {
				return &usageError{fmt.Errorf("--update-mask names %s but --body-file sets no %s", path, f)}
			}
		}
	}
	return nil
}

// bodyField returns obj's value for field in any spelling.
func bodyField(obj map[string]any, field string) (any, bool) {
	want := foldMaskName(field)
	for k, v := range obj {
		if foldMaskName(k) == want {
			return v, true
		}
	}
	return nil, false
}
