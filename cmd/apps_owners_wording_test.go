package cmd

import (
	"regexp"
	"strings"
	"testing"
)

// appOwnersPopulateClaimTriggers are phrases that, paired with "appOwners"
// nearby, assert owners become visible there. Measured against the lab tenant
// twice: 0 of 47 apps on the first pass, 0 of 46 on the second (the first
// pass's scratch apps had been deleted by then). appOwners never populates,
// while GET .../ownerids does converge (on the order of minutes). This
// wording drifted, wrong, across five files before anything caught it.
// "fills"/" fill " cover the verb agents.md uses ("wait for that field to fill").
var appOwnersPopulateClaimTriggers = []string{"appear", "show up", "shows up", "populat", "fills", " fill "}

// appOwnersNegations are the words that turn a trigger phrase into the
// correct claim ("appOwners does NOT populate") instead of the false one.
var appOwnersNegations = []string{"not ", "n't", "never", "no longer"}

// appOwnersMaxPairDistance bounds how far apart (in characters, after
// normalization) an "appOwners" mention and a trigger phrase can be and
// still plausibly describe the same claim. Past this they're treated as
// unrelated mentions in the same text, not a pairing to evaluate.
const appOwnersMaxPairDistance = 80

// appOwnersNegationPad is how far outside the appOwners/trigger span itself
// a negation is allowed to sit and still count as negating that pairing.
// Deliberately small: it must cover an immediately preceding negation like
// "does not show up" or "no longer populates" (trigger right after the
// negation), not a negation that belongs to an earlier, unrelated clause
// ("The old flag is no longer supported, and appOwners populates..." --
// "no longer" there negates "supported", not "populates", even though it's
// only ~17 chars from "appOwners").
const appOwnersNegationPad = 15

var ellipsisRun = regexp.MustCompile(`\.\.\.+`)
var whitespaceRun = regexp.MustCompile(`\s+`)

// normalizeForClaimScan collapses whitespace (Long/Markdown text wraps at 80
// cols, so a trigger phrase like "show up" can land as "show\nup") and
// strips this repo's "..." path notation (e.g. "GET .../ownerids"), which
// would otherwise inflate the character distance between an appOwners
// mention and a trigger and could, depending on how that distance is used,
// hide a real pairing.
func normalizeForClaimScan(text string) string {
	text = ellipsisRun.ReplaceAllString(text, "")
	text = whitespaceRun.ReplaceAllString(text, " ")
	return strings.ToLower(text)
}

// findAppOwnersPopulateClaims scans text for every place where "appOwners"
// and a populate/appear/show-up trigger occur within appOwnersMaxPairDistance
// characters of each other, and returns one entry per such pairing that has
// no negation word within appOwnersNegationPad characters of the pair span.
//
// This works over a character-proximity window around each (appOwners,
// trigger) pair rather than splitting into "sentences" first and checking
// for negation anywhere in the sentence. The sentence-based approach had two
// independent bypasses: a negation in an unrelated clause of the same
// sentence suppressed a real claim elsewhere in it, and this repo's own
// "..." path notation split one sentence into two fragments -- one holding
// the trigger, the other holding "appOwners" -- so neither fragment alone
// tripped the check. Requiring proximity between the trigger and "appOwners"
// themselves, and requiring the negation to be adjacent to THAT pairing
// (not merely present somewhere in the same sentence), closes both: a
// negation belonging to a different clause is normally far enough from the
// pairing to fall outside appOwnersNegationPad, and there is no sentence
// boundary left for "..." to exploit.
func findAppOwnersPopulateClaims(text string) []string {
	lower := normalizeForClaimScan(text)

	find := func(substr string) [][2]int {
		var spans [][2]int
		for i := 0; ; {
			idx := strings.Index(lower[i:], substr)
			if idx < 0 {
				break
			}
			start := i + idx
			end := start + len(substr)
			spans = append(spans, [2]int{start, end})
			i = end
		}
		return spans
	}

	aoSpans := find("appowners")
	if len(aoSpans) == 0 {
		return nil
	}

	var trigSpans [][2]int
	for _, trig := range appOwnersPopulateClaimTriggers {
		trigSpans = append(trigSpans, find(trig)...)
	}
	if len(trigSpans) == 0 {
		return nil
	}

	seen := map[[2]int]bool{}
	var violations []string
	for _, ao := range aoSpans {
		for _, tr := range trigSpans {
			spanStart, spanEnd := ao[0], ao[1]
			if tr[0] < spanStart {
				spanStart = tr[0]
			}
			if tr[1] > spanEnd {
				spanEnd = tr[1]
			}
			if spanEnd-spanStart > appOwnersMaxPairDistance {
				continue // too far apart to plausibly describe the same claim
			}
			if seen[[2]int{spanStart, spanEnd}] {
				continue
			}

			winStart := spanStart - appOwnersNegationPad
			if winStart < 0 {
				winStart = 0
			}
			winEnd := spanEnd + appOwnersNegationPad
			if winEnd > len(lower) {
				winEnd = len(lower)
			}
			window := lower[winStart:winEnd]

			negated := false
			for _, neg := range appOwnersNegations {
				if strings.Contains(window, neg) {
					negated = true
					break
				}
			}
			if negated {
				continue
			}

			seen[[2]int{spanStart, spanEnd}] = true
			ctxStart := spanStart - 30
			if ctxStart < 0 {
				ctxStart = 0
			}
			ctxEnd := spanEnd + 30
			if ctxEnd > len(lower) {
				ctxEnd = len(lower)
			}
			violations = append(violations, strings.TrimSpace(lower[ctxStart:ctxEnd]))
		}
	}
	return violations
}

// TestAppOwnersNeverClaimedToPopulate guards the four user/agent-facing
// surfaces that have carried the false claim that new owners show up in
// "apps get"'s appOwners field: the set-owners command's Long, its success
// message, and the embedded onboarding guide (docs_guide.go and the
// embedded cmd/agents.md).
func TestAppOwnersNeverClaimedToPopulate(t *testing.T) {
	surfaces := map[string]string{
		"apps set-owners --help (Long)":   appsSetOwnersCmd.Long,
		"apps set-owners success message": setOwnersSuccessFmt,
		"docs guide: configure-new-app":   guideConfigureNewApp,
		"cmd/agents.md (embedded)":        agentsTemplate,
	}
	for name, text := range surfaces {
		if violations := findAppOwnersPopulateClaims(text); len(violations) > 0 {
			t.Errorf("%s claims appOwners populates/shows owners:\n%s", name, strings.Join(violations, "\n"))
		}
	}
}

// TestFindAppOwnersPopulateClaims pins the specific bypasses two independent
// reviews found in an earlier version of this detector (sentence-wide
// negation, and "..." path notation splitting one claim into two harmless-
// looking fragments), so they can never silently regress, alongside cases
// that must NOT be flagged.
func TestFindAppOwnersPopulateClaims(t *testing.T) {
	tests := []struct {
		name       string
		text       string
		wantCaught bool
	}{
		{
			name:       "unrelated negation earlier in sentence, trigger before appOwners",
			text:       `Owners are not created here, but the new owners show up in the appOwners field within 60-90s.`,
			wantCaught: true,
		},
		{
			name:       "unrelated negation earlier in sentence, appOwners before trigger",
			text:       `This endpoint is never called directly by users, and the appOwners field shows up the new owner shortly after.`,
			wantCaught: true,
		},
		{
			name:       "no longer negates a different clause, not the populate claim",
			text:       `The old flag is no longer supported, and appOwners populates within a couple minutes for most apps.`,
			wantCaught: true,
		},
		{
			name:       "don't negates a different clause, not the show-up claim",
			text:       `We don't recommend polling too fast, but appOwners does show up the new owner within a minute or two.`,
			wantCaught: true,
		},
		{
			name:       "... path notation must not fragment the claim into two harmless halves",
			text:       `New owners show up in GET .../ownerids and eventually in appOwners too.`,
			wantCaught: true,
		},
		{
			name:       "adjacent negation directly before the trigger",
			text:       `The "appOwners" field in "apps get" is not populated by this API -- don't use it to check ownership.`,
			wantCaught: false,
		},
		{
			name:       "adjacent negation between appOwners and the trigger",
			text:       "`apps get`'s `appOwners` field never populates -- check `ownerids` instead.",
			wantCaught: false,
		},
		{
			name:       "trigger and negation both present but far from any appOwners mention",
			text:       `For owners, don't trust the appOwners field embedded in the app object (from "c1i apps get <id>") as the source of truth: in testing across every app in a tenant, it never populated, even long after "apps set-owners --wait" had already reported success.`,
			wantCaught: false,
		},
		{
			name:       "no appOwners mention at all",
			text:       `Owner changes take a couple of minutes to show up in GET .../ownerids.`,
			wantCaught: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			violations := findAppOwnersPopulateClaims(tt.text)
			caught := len(violations) > 0
			if caught != tt.wantCaught {
				t.Errorf("findAppOwnersPopulateClaims(%q) caught=%v (violations=%v), want caught=%v",
					tt.text, caught, violations, tt.wantCaught)
			}
		})
	}
}
