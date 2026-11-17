package cmd

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

// updateField is one updatable field of an update command: the flags it reads
// and how they land in the request object. apply returns the update-mask
// paths it added. A nil apply declares flags a command's own builder reads
// (update-credentials); the table still names them for the "nothing to
// update" message and the coverage test.
type updateField struct {
	flags []string
	apply func(cmd *cobra.Command, obj map[string]any) (paths []string, err error)
}

// updateSpec is one update command's field table, in mask order. Only
// updateSpecs and wrapUpdate may name the request's "updateMask" key
// (TestUpdateCommandsDoNotBuildMasksByHand).
type updateSpec struct {
	fields []updateField
	// alt is a non-field way to run the command, named in the error
	// ("--body-file with --update-mask").
	alt string
	// scope are the command's other flags (parents, --body-file, ...), so the
	// tree test can tell them from a field the table forgot.
	scope []string
}

// updateSpecs maps each update command to its table(s); a command whose
// fields depend on a mode (update-credentials: hosted or external) has one
// per mode.
var updateSpecs = map[*cobra.Command][]*updateSpec{}

func registerUpdateSpec(cmd *cobra.Command, s ...*updateSpec) { updateSpecs[cmd] = s }

// strUpdate sends a set string flag under key; validate may refuse a value.
// "" is sent as is: whether that is allowed is allowEmpty's rule.
func strUpdate(flag, key string, validate ...func(string) error) updateField {
	return updateField{flags: []string{flag}, apply: func(cmd *cobra.Command, obj map[string]any) ([]string, error) {
		if !cmd.Flags().Changed(flag) {
			return nil, nil
		}
		v := mustGetString(cmd, flag)
		for _, check := range validate {
			if err := check(v); err != nil {
				return nil, err
			}
		}
		obj[key] = v
		return []string{key}, nil
	}}
}

// boolUpdate sends a set bool flag; an explicit false is a change.
func boolUpdate(flag, key string) updateField {
	return updateField{flags: []string{flag}, apply: func(cmd *cobra.Command, obj map[string]any) ([]string, error) {
		if !cmd.Flags().Changed(flag) {
			return nil, nil
		}
		obj[key], _ = cmd.Flags().GetBool(flag)
		return []string{key}, nil
	}}
}

// optionalBool is the API's tri-state bool enum for a flag that was set.
func optionalBool(on bool) string {
	if on {
		return "OPTIONAL_BOOL_TRUE"
	}
	return "OPTIONAL_BOOL_FALSE"
}

// optionalBoolUpdate sends a set bool flag as an OPTIONAL_BOOL_* enum.
func optionalBoolUpdate(flag, key string) updateField {
	return updateField{flags: []string{flag}, apply: func(cmd *cobra.Command, obj map[string]any) ([]string, error) {
		if !cmd.Flags().Changed(flag) {
			return nil, nil
		}
		on, _ := cmd.Flags().GetBool(flag)
		obj[key] = optionalBool(on)
		return []string{key}, nil
	}}
}

// enumUpdate sends a set string flag normalized through e.
func enumUpdate(flag, key string, e *enumFlag) updateField {
	return updateField{flags: []string{flag}, apply: func(cmd *cobra.Command, obj map[string]any) ([]string, error) {
		if !cmd.Flags().Changed(flag) {
			return nil, nil
		}
		v, err := e.parse(mustGetString(cmd, flag))
		if err != nil {
			return nil, err
		}
		obj[key] = v
		return []string{key}, nil
	}}
}

// durationUpdate sends a set addProtoDurationFlag flag in protobuf form.
func durationUpdate(flag, key string) updateField {
	return updateField{flags: []string{flag}, apply: func(cmd *cobra.Command, obj map[string]any) ([]string, error) {
		v, ok, err := protoDurationFlag(cmd, flag)
		if err != nil || !ok {
			return nil, err
		}
		obj[key] = v
		return []string{key}, nil
	}}
}

// hookUpdate is a field with its own logic over flags.
func hookUpdate(flags []string, apply func(cmd *cobra.Command, obj map[string]any) ([]string, error)) updateField {
	return updateField{flags: flags, apply: apply}
}

// apply runs every field in order against obj and returns the mask paths.
func (s *updateSpec) apply(cmd *cobra.Command, obj map[string]any) ([]string, error) {
	var paths []string
	for _, f := range s.fields {
		if f.apply == nil {
			continue
		}
		p, err := f.apply(cmd, obj)
		if err != nil {
			return nil, err
		}
		paths = append(paths, p...)
	}
	return paths, nil
}

// build is apply plus the mask string; no path at all is a usage error that
// names every field flag of the table.
func (s *updateSpec) build(cmd *cobra.Command, obj map[string]any) (string, error) {
	paths, err := s.apply(cmd, obj)
	if err != nil {
		return "", err
	}
	if len(paths) == 0 {
		return "", s.nothingToUpdate()
	}
	return strings.Join(paths, ","), nil
}

// flagNames lists the table's flags, in order.
func (s *updateSpec) flagNames() []string {
	var out []string
	for _, f := range s.fields {
		out = append(out, f.flags...)
	}
	return out
}

func (s *updateSpec) nothingToUpdate() error {
	var opts []string
	for _, n := range s.flagNames() {
		opts = append(opts, "--"+n)
	}
	if s.alt != "" {
		opts = append(opts, s.alt)
	}
	list := opts[0]
	switch {
	case len(opts) == 2:
		list += " or " + opts[1]
	case len(opts) > 2:
		list = strings.Join(opts[:len(opts)-1], ", ") + ", or " + opts[len(opts)-1]
	}
	return &usageError{fmt.Errorf("nothing to update: pass %s", list)}
}

// wrapUpdate is the request every update endpoint takes: the object under
// key plus its mask.
func wrapUpdate(key string, obj map[string]any, mask string) map[string]any {
	body := map[string]any{key: obj}
	setUpdateMask(body, mask)
	return body
}

func setUpdateMask(body map[string]any, mask string) { body["updateMask"] = mask }

// deriveCredentialUpdateMask returns the update_mask for an UpdateCredentials
// request built from the convenience flags: the sorted set of top-level field
// paths present in cfg.
//
// The backend applies a credential/config change only when the mask names that
// field by its proto path — for the auth oneof that is the *case name* (e.g.
// "bearerToken", "none", "customHeader", "basicAuth", "oauth2"), NOT the
// wrapping "hostedConfig"/"externalConfig". The config builders already key the
// object by those exact paths, so the mask is simply cfg's keys. (Sending the
// wrapper name — the previous behavior — matched no path, so the backend
// silently dropped every credential change.) Mirrors the admin UI, which masks
// exactly the fields the user edited.
func deriveCredentialUpdateMask(cfg map[string]any) string {
	return strings.Join(sortedConfigKeys(cfg), ",")
}

func sortedConfigKeys(cfg map[string]any) []string {
	keys := make([]string, 0, len(cfg))
	for k := range cfg {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// checkDerivedMaskKeys refuses a config key that would become an empty or
// malformed --update-mask path, naming the key.
func checkDerivedMaskKeys(cfg map[string]any) error {
	for i, k := range sortedConfigKeys(cfg) {
		if !hasVisibleRune(k) {
			return &usageError{fmt.Errorf("config key %d is empty; it would become an empty --update-mask path", i+1)}
		}
		if _, bad := splitPathSegments(k); bad != 0 {
			return &usageError{fmt.Errorf("config key %q has an empty dot-segment (segment %d); it would become an invalid --update-mask path", k, bad)}
		}
	}
	return nil
}
