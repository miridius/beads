//go:build regression

// upgrade_test.go tests the actual upgrade scenario: data created by the
// baseline binary is opened and operated on by the candidate binary.
//
// Unlike scenarios_test.go (which runs the same commands on both binaries
// independently), these tests verify that an existing database created by
// an older version survives being opened by a newer version — the exact
// scenario users worry about when upgrading production projects.
package regression

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// upgradeWorkspace creates data with the baseline binary, then returns a
// workspace that uses the candidate binary on the same directory. This
// simulates the user upgrading bd in place.
func upgradeWorkspace(t *testing.T, setup func(w *workspace)) *workspace {
	t.Helper()

	// Phase 1: create data with baseline
	base := newWorkspace(t, baselineBin)
	setup(base)

	// Phase 2: swap to candidate binary on the same directory
	upgraded := &workspace{
		dir:        base.dir,
		bdPath:     candidateBin,
		t:          t,
		createdIDs: base.createdIDs,
	}
	return upgraded
}

// snapshotIssues returns a map of id → parsed issue from bd list + bd show.
func snapshotIssues(t *testing.T, w *workspace) map[string]map[string]any {
	t.Helper()
	raw := w.snapshot()
	if raw == "" {
		return nil
	}
	result := make(map[string]map[string]any)
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			continue
		}
		if id, ok := m["id"].(string); ok {
			result[id] = m
		}
	}
	return result
}

// requireField asserts a string field has the expected value.
func requireField(t *testing.T, issue map[string]any, field, expected, context string) {
	t.Helper()
	got, _ := issue[field].(string)
	if got != expected {
		t.Errorf("%s: %s = %q, want %q", context, field, got, expected)
	}
}

// requireLabels asserts the issue has exactly the expected labels (order-insensitive).
func requireLabels(t *testing.T, issue map[string]any, expected []string, context string) {
	t.Helper()
	raw, _ := issue["labels"].([]any)
	var got []string
	for _, v := range raw {
		if s, ok := v.(string); ok {
			got = append(got, s)
		}
	}
	sort.Strings(got)
	sort.Strings(expected)
	if strings.Join(got, ",") != strings.Join(expected, ",") {
		t.Errorf("%s: labels = %v, want %v", context, got, expected)
	}
}

// requireDepCount asserts the issue has the expected number of dependencies.
func requireDepCount(t *testing.T, issue map[string]any, field string, expected int, context string) {
	t.Helper()
	deps, _ := issue[field].([]any)
	if len(deps) != expected {
		t.Errorf("%s: %s count = %d, want %d", context, field, len(deps), expected)
	}
}

// requireCommentCount asserts the issue has the expected number of comments.
func requireCommentCount(t *testing.T, issue map[string]any, expected int, context string) {
	t.Helper()
	comments, _ := issue["comments"].([]any)
	if len(comments) != expected {
		t.Errorf("%s: comment count = %d, want %d", context, len(comments), expected)
	}
}

// ---------------------------------------------------------------------------
// Data survival tests: verify data created by baseline survives upgrade
// ---------------------------------------------------------------------------

// TestUpgradeDataSurvival creates a rich dataset with the baseline binary
// and verifies every field survives when read by the candidate binary.
func TestUpgradeDataSurvival(t *testing.T) {
	var id1, id2, id3 string

	upgraded := upgradeWorkspace(t, func(w *workspace) {
		id1 = w.create(
			"--title", "Rich issue",
			"--type", "feature",
			"--priority", "1",
			"--description", "Detailed description",
			"--design", "Layered architecture",
			"--acceptance", "All tests pass",
			"--notes", "Check with team first",
			"--assignee", "alice",
		)
		id2 = w.create("--title", "Dependency target", "--type", "task", "--priority", "2")
		id3 = w.create("--title", "Closed issue", "--type", "bug", "--priority", "3")

		w.run("label", "add", id1, "critical")
		w.run("label", "add", id1, "backend")
		w.run("dep", "add", id1, id2)
		w.run("comment", id1, "First review comment")
		w.run("comment", id1, "Second review comment")
		w.run("close", id3, "--reason", "wontfix")
	})

	// Read all data with candidate binary
	issues := snapshotIssues(t, upgraded)
	if len(issues) == 0 {
		t.Fatal("candidate binary returned no issues from baseline database")
	}

	// Verify issue 1: rich fields
	issue1 := issues[id1]
	if issue1 == nil {
		t.Fatalf("issue %s not found after upgrade", id1)
	}
	requireField(t, issue1, "title", "Rich issue", "issue1")
	requireField(t, issue1, "type", "feature", "issue1")
	requireField(t, issue1, "description", "Detailed description", "issue1")
	requireField(t, issue1, "design", "Layered architecture", "issue1")
	requireField(t, issue1, "acceptance", "All tests pass", "issue1")
	requireField(t, issue1, "notes", "Check with team first", "issue1")
	requireField(t, issue1, "assignee", "alice", "issue1")
	requireField(t, issue1, "status", "open", "issue1")
	requireLabels(t, issue1, []string{"backend", "critical"}, "issue1")
	requireDepCount(t, issue1, "dependencies", 1, "issue1")
	requireCommentCount(t, issue1, 2, "issue1")

	// Verify priority survived (numeric field)
	if p, ok := issue1["priority"].(float64); !ok || p != 1 {
		t.Errorf("issue1: priority = %v, want 1", issue1["priority"])
	}

	// Verify issue 3: closed status survived
	issue3 := issues[id3]
	if issue3 == nil {
		t.Fatalf("issue %s not found after upgrade", id3)
	}
	requireField(t, issue3, "status", "closed", "issue3")
}

// TestUpgradePreservesDependencyGraph creates a dependency graph with baseline
// and verifies ready/blocked semantics work correctly after upgrade.
func TestUpgradePreservesDependencyGraph(t *testing.T) {
	var ids [5]string

	upgraded := upgradeWorkspace(t, func(w *workspace) {
		ids[0] = w.create("--title", "Foundation", "--type", "task", "--priority", "1")
		ids[1] = w.create("--title", "Blocked by foundation", "--type", "task", "--priority", "2")
		ids[2] = w.create("--title", "Transitively blocked", "--type", "task", "--priority", "2")
		ids[3] = w.create("--title", "Independent", "--type", "task", "--priority", "3")
		ids[4] = w.create("--title", "Unblocked after close", "--type", "task", "--priority", "2")

		w.run("dep", "add", ids[1], ids[0])
		w.run("dep", "add", ids[2], ids[1])
		w.run("dep", "add", ids[4], ids[0])

		w.run("close", ids[0], "--reason", "done")
	})

	// Verify ready semantics with candidate binary
	ready := parseReadyIDs(t, upgraded)

	// ids[0] is closed — not ready
	if ready[ids[0]] {
		t.Errorf("ids[0] (closed) should not be ready")
	}
	// ids[1] should be ready (dependency ids[0] is closed)
	if !ready[ids[1]] {
		t.Errorf("ids[1] should be ready (blocker closed)")
	}
	// ids[2] is transitively blocked by ids[1] (still open)
	if ready[ids[2]] {
		t.Errorf("ids[2] should NOT be ready (transitively blocked)")
	}
	// ids[3] has no deps — should be ready
	if !ready[ids[3]] {
		t.Errorf("ids[3] (independent) should be ready")
	}
	// ids[4] depends on ids[0] which is closed — should be ready
	if !ready[ids[4]] {
		t.Errorf("ids[4] should be ready (blocker closed)")
	}
}

// TestUpgradePreservesLabels creates issues with labels using baseline and
// verifies label operations work correctly after upgrade.
func TestUpgradePreservesLabels(t *testing.T) {
	var id string

	upgraded := upgradeWorkspace(t, func(w *workspace) {
		id = w.create("--title", "Labeled issue", "--type", "task")
		w.run("label", "add", id, "frontend")
		w.run("label", "add", id, "urgent")
		w.run("label", "add", id, "v2")
	})

	// Verify labels survived
	issues := snapshotIssues(t, upgraded)
	issue := issues[id]
	if issue == nil {
		t.Fatalf("issue %s not found after upgrade", id)
	}
	requireLabels(t, issue, []string{"frontend", "urgent", "v2"}, "pre-upgrade labels")

	// Add a new label with candidate binary
	upgraded.run("label", "add", id, "post-upgrade")
	// Remove an old label
	upgraded.run("label", "remove", id, "urgent")

	issues = snapshotIssues(t, upgraded)
	issue = issues[id]
	requireLabels(t, issue, []string{"frontend", "post-upgrade", "v2"}, "post-upgrade labels")
}

// TestUpgradePreservesComments creates commented issues with baseline and
// verifies comments survive and new comments can be added after upgrade.
func TestUpgradePreservesComments(t *testing.T) {
	var id string

	upgraded := upgradeWorkspace(t, func(w *workspace) {
		id = w.create("--title", "Commented issue", "--type", "task")
		w.run("comment", id, "Pre-upgrade comment 1")
		w.run("comment", id, "Pre-upgrade comment 2")
	})

	// Verify pre-upgrade comments survived
	issues := snapshotIssues(t, upgraded)
	issue := issues[id]
	if issue == nil {
		t.Fatalf("issue %s not found after upgrade", id)
	}
	requireCommentCount(t, issue, 2, "pre-upgrade comments")

	// Add a new comment with candidate binary
	upgraded.run("comment", id, "Post-upgrade comment")

	issues = snapshotIssues(t, upgraded)
	issue = issues[id]
	requireCommentCount(t, issue, 3, "post-upgrade comments")
}

// ---------------------------------------------------------------------------
// Post-upgrade CRUD tests: verify operations work on upgraded database
// ---------------------------------------------------------------------------

// TestUpgradeThenCreate verifies new issues can be created in an upgraded database.
func TestUpgradeThenCreate(t *testing.T) {
	upgraded := upgradeWorkspace(t, func(w *workspace) {
		w.create("--title", "Pre-upgrade issue", "--type", "task")
	})

	// Create a new issue with candidate binary
	newID := upgraded.create("--title", "Post-upgrade issue", "--type", "feature", "--priority", "1")

	issues := snapshotIssues(t, upgraded)
	if len(issues) != 2 {
		t.Errorf("expected 2 issues, got %d", len(issues))
	}
	newIssue := issues[newID]
	if newIssue == nil {
		t.Fatalf("new issue %s not found", newID)
	}
	requireField(t, newIssue, "title", "Post-upgrade issue", "new issue")
	requireField(t, newIssue, "type", "feature", "new issue")
}

// TestUpgradeThenUpdate verifies updates work on pre-upgrade issues.
func TestUpgradeThenUpdate(t *testing.T) {
	var id string

	upgraded := upgradeWorkspace(t, func(w *workspace) {
		id = w.create(
			"--title", "Original title",
			"--type", "task",
			"--priority", "2",
			"--description", "Original description",
			"--notes", "Original notes",
		)
	})

	// Update with candidate binary — only change title
	upgraded.run("update", id, "--title", "Updated title")

	issues := snapshotIssues(t, upgraded)
	issue := issues[id]
	if issue == nil {
		t.Fatalf("issue %s not found after upgrade", id)
	}

	// Title should be updated
	requireField(t, issue, "title", "Updated title", "post-update")
	// Other fields must survive the update (no clobbering)
	requireField(t, issue, "description", "Original description", "post-update")
	requireField(t, issue, "notes", "Original notes", "post-update")
	requireField(t, issue, "type", "task", "post-update")
	if p, ok := issue["priority"].(float64); !ok || p != 2 {
		t.Errorf("post-update: priority = %v, want 2", issue["priority"])
	}
}

// TestUpgradeThenClose verifies close/reopen cycle works on pre-upgrade issues.
func TestUpgradeThenClose(t *testing.T) {
	var id string

	upgraded := upgradeWorkspace(t, func(w *workspace) {
		id = w.create("--title", "Closeable issue", "--type", "bug", "--priority", "1")
	})

	// Close with candidate binary
	upgraded.run("close", id, "--reason", "fixed in upgrade")

	issues := snapshotIssues(t, upgraded)
	issue := issues[id]
	if issue == nil {
		t.Fatalf("issue %s not found", id)
	}
	requireField(t, issue, "status", "closed", "post-close")

	// Reopen
	upgraded.run("reopen", id)

	issues = snapshotIssues(t, upgraded)
	issue = issues[id]
	requireField(t, issue, "status", "open", "post-reopen")
	// Verify fields survived the close/reopen cycle
	requireField(t, issue, "title", "Closeable issue", "post-reopen")
	requireField(t, issue, "type", "bug", "post-reopen")
}

// TestUpgradeThenAddDeps verifies new dependencies can be created between
// pre-upgrade and post-upgrade issues.
func TestUpgradeThenAddDeps(t *testing.T) {
	var preID string

	upgraded := upgradeWorkspace(t, func(w *workspace) {
		preID = w.create("--title", "Pre-upgrade blocker", "--type", "task")
	})

	// Create a new issue and add dependency to pre-upgrade issue
	postID := upgraded.create("--title", "Post-upgrade task", "--type", "task")
	upgraded.run("dep", "add", postID, preID)

	issues := snapshotIssues(t, upgraded)
	postIssue := issues[postID]
	if postIssue == nil {
		t.Fatalf("post-upgrade issue %s not found", postID)
	}
	requireDepCount(t, postIssue, "dependencies", 1, "cross-version dep")

	// Verify ready semantics: postID blocked by preID (open)
	ready := parseReadyIDs(t, upgraded)
	if ready[postID] {
		t.Errorf("post-upgrade issue should be blocked by pre-upgrade issue")
	}
	if !ready[preID] {
		t.Errorf("pre-upgrade issue should be ready (no deps)")
	}

	// Close the blocker and verify unblock
	upgraded.run("close", preID, "--reason", "done")
	ready = parseReadyIDs(t, upgraded)
	if !ready[postID] {
		t.Errorf("post-upgrade issue should be ready after blocker closed")
	}
}

// ---------------------------------------------------------------------------
// Differential upgrade tests: compare baseline-only vs upgraded snapshots
// ---------------------------------------------------------------------------

// TestUpgradeSnapshotParity creates identical data with baseline, snapshots
// it with baseline, then snapshots the same database with candidate. The
// normalized outputs should match.
func TestUpgradeSnapshotParity(t *testing.T) {
	// Create data with baseline
	base := newWorkspace(t, baselineBin)

	id1 := base.create("--title", "Task with everything", "--type", "feature", "--priority", "1",
		"--description", "Full description", "--assignee", "alice")
	id2 := base.create("--title", "Dependency", "--type", "task", "--priority", "2")
	id3 := base.create("--title", "Closed bug", "--type", "bug", "--priority", "3")

	base.run("label", "add", id1, "important")
	base.run("label", "add", id1, "v2")
	base.run("dep", "add", id1, id2)
	base.run("comment", id1, "Review note")
	base.run("close", id3, "--reason", "fixed")

	// Snapshot with baseline
	baselineSnap := base.snapshot()

	// Swap to candidate on same directory
	upgraded := &workspace{
		dir:        base.dir,
		bdPath:     candidateBin,
		t:          t,
		createdIDs: base.createdIDs,
	}

	// Snapshot with candidate
	candidateSnap := upgraded.snapshot()

	// Compare: same IDs, so use same ID map for both
	idMap := canonicalIDMap(base.createdIDs)
	diffNormalized(t, baselineSnap, candidateSnap, idMap, idMap)
}

// TestUpgradeBulkDataSurvival creates a larger dataset and verifies everything
// survives. This catches batch-processing bugs that only appear at scale.
func TestUpgradeBulkDataSurvival(t *testing.T) {
	var ids []string

	upgraded := upgradeWorkspace(t, func(w *workspace) {
		for i := 0; i < 15; i++ {
			id := w.create("--title", strings.ReplaceAll("Issue_NNN", "NNN", strings.Repeat("x", i+1)),
				"--type", "task", "--priority", string(rune('0'+i%5)))
			ids = append(ids, id)
			w.run("label", "add", id, "batch")
			w.run("comment", id, "Auto-generated comment")
		}
		// Dependency chain
		for i := 1; i < len(ids); i += 2 {
			w.run("dep", "add", ids[i], ids[i-1])
		}
		// Close some
		for i := 0; i < len(ids); i += 4 {
			w.run("close", ids[i], "--reason", "done")
		}
	})

	issues := snapshotIssues(t, upgraded)
	if len(issues) != 15 {
		t.Errorf("expected 15 issues after upgrade, got %d", len(issues))
	}

	// Verify each issue has its label and comment
	for _, id := range ids {
		issue := issues[id]
		if issue == nil {
			t.Errorf("issue %s missing after upgrade", id)
			continue
		}
		requireCommentCount(t, issue, 1, id)
		// Label should survive
		labels, _ := issue["labels"].([]any)
		if len(labels) == 0 {
			t.Errorf("issue %s: labels missing after upgrade", id)
		}
	}
}

// TestUpgradeParentChildHierarchy creates a parent-child hierarchy with baseline
// and verifies it survives and remains functional after upgrade.
func TestUpgradeParentChildHierarchy(t *testing.T) {
	var parentID, child1ID, child2ID string

	upgraded := upgradeWorkspace(t, func(w *workspace) {
		parentID = w.create("--title", "Epic parent", "--type", "epic", "--priority", "1")
		child1ID = w.create("--title", "Child one", "--type", "task", "--priority", "2")
		child2ID = w.create("--title", "Child two", "--type", "task", "--priority", "2")

		w.run("dep", "add", child1ID, parentID, "--type", "parent-child")
		w.run("dep", "add", child2ID, parentID, "--type", "parent-child")
	})

	// Verify parent-child relationships survived
	issues := snapshotIssues(t, upgraded)

	child1 := issues[child1ID]
	if child1 == nil {
		t.Fatalf("child1 %s missing after upgrade", child1ID)
	}
	requireDepCount(t, child1, "dependencies", 1, "child1 deps")

	child2 := issues[child2ID]
	if child2 == nil {
		t.Fatalf("child2 %s missing after upgrade", child2ID)
	}
	requireDepCount(t, child2, "dependencies", 1, "child2 deps")

	// Add a new child post-upgrade
	child3ID := upgraded.create("--title", "Child three", "--type", "task", "--priority", "3")
	upgraded.run("dep", "add", child3ID, parentID, "--type", "parent-child")

	issues = snapshotIssues(t, upgraded)
	child3 := issues[child3ID]
	if child3 == nil {
		t.Fatalf("child3 %s missing", child3ID)
	}
	requireDepCount(t, child3, "dependencies", 1, "child3 deps")
}

// TestUpgradeListFiltersParity verifies that list filters work correctly
// on data created by the baseline binary.
func TestUpgradeListFiltersParity(t *testing.T) {
	var openID, closedID string

	upgraded := upgradeWorkspace(t, func(w *workspace) {
		openID = w.create("--title", "Open task", "--type", "task", "--assignee", "alice")
		closedID = w.create("--title", "Closed bug", "--type", "bug", "--assignee", "bob")
		w.run("close", closedID, "--reason", "fixed")
	})

	// List all
	allSnap := upgraded.snapshot()
	allIssues := strings.Count(strings.TrimSpace(allSnap), "\n") + 1
	if allSnap == "" {
		allIssues = 0
	}
	if allIssues != 2 {
		t.Errorf("list --all: expected 2, got %d", allIssues)
	}

	// List open only
	openSnap := upgraded.snapshot("--status", "open")
	if openSnap == "" {
		t.Error("list --status open returned nothing")
	} else {
		openIssues := parseJSONLByID(t, openSnap)
		if _, ok := openIssues[openID]; !ok {
			t.Errorf("open issue %s missing from --status open", openID)
		}
		if _, ok := openIssues[closedID]; ok {
			t.Errorf("closed issue %s appeared in --status open", closedID)
		}
	}
}

// TestUpgradeDeferredSemantics verifies that deferred issues created by baseline
// remain deferred and excluded from ready after upgrade.
func TestUpgradeDeferredSemantics(t *testing.T) {
	var deferredID, normalID string

	upgraded := upgradeWorkspace(t, func(w *workspace) {
		deferredID = w.create("--title", "Deferred task", "--type", "task")
		normalID = w.create("--title", "Normal task", "--type", "task")
		w.run("update", deferredID, "--defer", "2099-12-31")
	})

	// Deferred should not appear in ready
	ready := parseReadyIDs(t, upgraded)
	if ready[deferredID] {
		t.Errorf("deferred issue should not be ready")
	}
	if !ready[normalID] {
		t.Errorf("normal issue should be ready")
	}
}
