package cmd

import (
	"strings"
	"testing"
)

// TestDocsSearchDocumentsNoRelevanceThreshold pins C53: docs search is semantic
// with no true no-match, so its help must warn that a returned hit is not proof
// a concept exists. Mintlify surfaces no score to key on, so the caveat is the
// fix; this guards it against a silent edit.
func TestDocsSearchDocumentsNoRelevanceThreshold(t *testing.T) {
	if !strings.Contains(docsSearchCmd.Long, "relevance threshold") {
		t.Error("docs search Long dropped the 'relevance threshold' caveat (C53)")
	}
	if !strings.Contains(docsSearchCmd.Long, "not proof") {
		t.Error("docs search Long dropped the 'a hit is not proof' warning (C53)")
	}
}
