package cmd

import (
	"strings"
	"testing"
)

// TestOwnerAsyncNoteInDocs ties the hand-copied provisioning-lag sentence in
// the docs to ownerAsyncWaitNote, so neither can be reworded alone. Rewrapping
// is allowed.
func TestOwnerAsyncNoteInDocs(t *testing.T) {
	want := flatten(ownerAsyncWaitNote)
	docs := map[string]string{
		"README.md":                     readDocFile(t, "../README.md"),
		"cmd/agents.md":                 agentsTemplate,
		"docs guide: configure-new-app": guideConfigureNewApp,
	}
	for name, doc := range docs {
		if !strings.Contains(flatten(doc), want) {
			t.Errorf("%s does not contain ownerAsyncWaitNote verbatim: %q", name, want)
		}
	}
}
