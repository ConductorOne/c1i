package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ConductorOne/c1i/internal/client"
)

// requiredFieldsFill copies each of fields from the object a GET of path (with
// query, which may be nil) holds under envelope into obj, where obj leaves it
// unset: the server rejects an update without them, even when the mask names
// none. label names the object in errors ("template"). The read also makes an
// unknown id fail before the send, --dry-run included.
func requiredFieldsFill(path string, query map[string]string, envelope, label string, fields []string, obj map[string]any) preRead {
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
		data, err := c.Get(ctx, path, query)
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
// no-op. A field set in two spellings is refused too.
func refuseMaskedFieldWithoutValue(mask string, obj map[string]any, fields []string) error {
	for _, f := range fields {
		if _, _, err := bodyField(obj, f); err != nil {
			return err
		}
	}
	for _, path := range strings.Split(mask, ",") {
		for _, f := range fields {
			if foldMaskName(path) == foldMaskName(f) && !bodySetsField(obj, f) {
				return &usageError{fmt.Errorf("--update-mask names %s but --body-file sets no %s", path, f)}
			}
		}
	}
	return nil
}
