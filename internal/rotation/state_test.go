package rotation

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"opencode2api/internal/config"
)

var (
	firstSOTA  = config.SOTAModels[0]
	secondSOTA = config.SOTAModels[1]
)

func testManager(t *testing.T, order []string) *Manager {
	t.Helper()
	cfg := config.RotationConfig{
		SOTA:  config.RotationGroup{Alias: "sota", Order: order},
		Sweet: config.RotationGroup{Alias: "sweet"},
	}
	m, err := New("", cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return m
}

func TestSyncBuildsGroupsAndOrder(t *testing.T) {
	m := testManager(t, nil)
	available := map[string]bool{"glm-5.3-flash": true, "sweet-a": true}
	for _, model := range config.SOTAModels {
		available[model] = true
	}
	m.Sync(available)

	snap := m.Snapshot()
	if len(snap.Groups) != 2 {
		t.Fatalf("groups = %d, want 2", len(snap.Groups))
	}
	sota, sweet := snap.Groups[0], snap.Groups[1]
	if sota.ID != "sota" || sota.Alias != "sota" {
		t.Fatalf("first group = %q/%q", sota.ID, sota.Alias)
	}
	if !sota.Available["glm-5.3"] || sota.Available["glm-5.3-flash"] {
		t.Fatalf("sota availability = %v", sota.Available)
	}
	if sota.Current != config.SOTAModels[0] {
		t.Fatalf("sota current = %q, want %q", sota.Current, config.SOTAModels[0])
	}
	if sweet.ID != "sweet" || !sweet.Available["glm-5.3-flash"] {
		t.Fatalf("sweet group = %+v", sweet)
	}
	want := []string{"glm-5.3-flash", "sweet-a"}
	if len(sweet.Order) != len(want) || sweet.Order[0] != want[0] || sweet.Order[1] != want[1] {
		t.Fatalf("sweet order = %v, want %v", sweet.Order, want)
	}
	if sweet.Current != "glm-5.3-flash" {
		t.Fatalf("sweet current = %q", sweet.Current)
	}
}

func TestSelectSkipsVisitedAndPaused(t *testing.T) {
	m := testManager(t, []string{firstSOTA, secondSOTA})
	m.Sync(map[string]bool{firstSOTA: true, secondSOTA: true})

	tok, ok := m.Select("sota", nil)
	if !ok || tok.Model != firstSOTA {
		t.Fatalf("first select = %+v, %v", tok, ok)
	}
	if _, ok := m.Select("sota", map[string]bool{firstSOTA: true, secondSOTA: true}); ok {
		t.Fatal("select returned a visited model")
	}
	next, ok := m.Select("sota", map[string]bool{firstSOTA: true})
	if !ok || next.Model != secondSOTA {
		t.Fatalf("skip visited = %+v, %v", next, ok)
	}

	m.Fail(tok, time.Minute, "test")
	snap := m.Snapshot()
	if snap.Groups[0].Current != secondSOTA {
		t.Fatalf("current = %q after fail, want %q", snap.Groups[0].Current, secondSOTA)
	}
	if until := snap.Groups[0].Paused[firstSOTA]; !until.After(time.Now()) {
		t.Fatalf("pause = %v, want future", until)
	}
	after, ok := m.Select("sota", nil)
	if !ok || after.Model != secondSOTA {
		t.Fatalf("select after fail = %+v, %v", after, ok)
	}
}

func TestFailIgnoresStaleGeneration(t *testing.T) {
	m := testManager(t, []string{firstSOTA, secondSOTA})
	m.Sync(map[string]bool{firstSOTA: true, secondSOTA: true})
	tok, ok := m.Select("sota", nil)
	if !ok {
		t.Fatal("select failed")
	}
	if err := m.Manual("sota", secondSOTA); err != nil {
		t.Fatalf("manual switch: %v", err)
	}
	m.Fail(tok, time.Minute, "late failure")

	snap := m.Snapshot()
	if snap.Groups[0].Current != secondSOTA {
		t.Fatalf("stale fail moved current to %q", snap.Groups[0].Current)
	}
	if snap.Groups[0].Paused[firstSOTA].IsZero() {
		t.Fatal("stale fail did not pause the failed model")
	}
}

func TestManualRejectsUnavailableModel(t *testing.T) {
	m := testManager(t, []string{firstSOTA, secondSOTA})
	m.Sync(map[string]bool{firstSOTA: true})

	if err := m.Manual("sota", secondSOTA); err == nil {
		t.Fatal("manual accepted an unavailable model")
	}
	if err := m.Manual("sota", firstSOTA); err != nil {
		t.Fatalf("manual first model: %v", err)
	}
	if got := m.Snapshot().Groups[0].Current; got != firstSOTA {
		t.Fatalf("current = %q, want %q", got, firstSOTA)
	}
}

func TestConfigureAppendsRemovedModelsAndKeepsCurrent(t *testing.T) {
	m := testManager(t, []string{firstSOTA, secondSOTA})
	m.Sync(map[string]bool{firstSOTA: true, secondSOTA: true})
	if err := m.Manual("sota", secondSOTA); err != nil {
		t.Fatalf("manual switch: %v", err)
	}
	m.Configure(config.RotationConfig{
		SOTA:  config.RotationGroup{Alias: "sota", Order: []string{secondSOTA}},
		Sweet: config.RotationGroup{Alias: "sweet"},
	})

	g := m.Snapshot().Groups[0]
	if len(g.Order) != 2 || g.Order[0] != secondSOTA || g.Order[1] != firstSOTA {
		t.Fatalf("order = %v, want [%s %s]", g.Order, secondSOTA, firstSOTA)
	}
	if g.Current != secondSOTA {
		t.Fatalf("current = %q, want %q", g.Current, secondSOTA)
	}
}

func TestPersistenceRoundTripAndInvalidFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rotation.json")
	cfg := config.RotationConfig{
		SOTA:  config.RotationGroup{Alias: "sota", Order: []string{firstSOTA, secondSOTA}},
		Sweet: config.RotationGroup{Alias: "sweet"},
	}
	m, err := New(path, cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	m.Sync(map[string]bool{firstSOTA: true, secondSOTA: true})

	reloaded, err := New(path, cfg)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	g := reloaded.Snapshot().Groups[0]
	if g.Current != firstSOTA || len(g.Order) != 2 || g.Order[0] != firstSOTA || g.Order[1] != secondSOTA {
		t.Fatalf("reloaded group = %+v", g)
	}

	bad := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(bad, []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(bad, cfg); err == nil {
		t.Fatal("New accepted invalid JSON")
	}
}
