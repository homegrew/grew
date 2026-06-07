package resolver

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/homegrew/grew/pkg/depgraph"
	"github.com/homegrew/grew/pkg/formula"
)

// TestDepChainResolution loads fixture formulas and asserts correct topological order.
func TestDepChainResolution(t *testing.T) {
	// Find fixture directory relative to this file
	_, thisFile, _, _ := runtime.Caller(0)
	fixtureDir := filepath.Join(filepath.Dir(thisFile), "../../tests/fixtures/formulas")

	// Load fixtures
	leaf, err := loadFixture(filepath.Join(fixtureDir, "leaf.yaml"))
	if err != nil {
		t.Skipf("skipping: leaf fixture not found (%v)", err)
	}
	mid, err := loadFixture(filepath.Join(fixtureDir, "mid.yaml"))
	if err != nil {
		t.Skipf("skipping: mid fixture not found (%v)", err)
	}
	top, err := loadFixture(filepath.Join(fixtureDir, "top.yaml"))
	if err != nil {
		t.Skipf("skipping: top fixture not found (%v)", err)
	}

	// Build graph
	g := depgraph.New()
	g.AddFormula(leaf)
	g.AddFormula(mid)
	g.AddFormula(top)

	// Resolve
	sorted, err := g.TopologicalSort()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Assert order: leaf < mid < top, leaf appears once
	order := make(map[string]int)
	count := make(map[string]int)
	for i, name := range sorted {
		order[name] = i
		count[name]++
	}

	if count["leaf"] != 1 {
		t.Errorf("leaf should appear exactly once, got %d", count["leaf"])
	}
	if order["leaf"] >= order["mid"] {
		t.Errorf("leaf should precede mid")
	}
	if order["mid"] >= order["top"] {
		t.Errorf("mid should precede top")
	}
}

// TestCycleDetection loads cycle fixtures and asserts error.
func TestCycleDetection(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	fixtureDir := filepath.Join(filepath.Dir(thisFile), "../../tests/fixtures/formulas")

	cycleA, err := loadFixture(filepath.Join(fixtureDir, "cycle-a.yaml"))
	if err != nil {
		t.Skipf("skipping: cycle-a fixture not found (%v)", err)
	}
	cycleB, err := loadFixture(filepath.Join(fixtureDir, "cycle-b.yaml"))
	if err != nil {
		t.Skipf("skipping: cycle-b fixture not found (%v)", err)
	}

	g := depgraph.New()
	g.AddFormula(cycleA)
	g.AddFormula(cycleB)

	_, err = g.TopologicalSort()
	if !errors.Is(err, depgraph.ErrCycleDetected) {
		t.Errorf("expected ErrCycleDetected, got %v", err)
	}

	cycles := g.DetectCycles()
	if len(cycles) == 0 {
		t.Fatal("expected at least one cycle")
	}
	// Both cycle-a and cycle-b should appear in the cycle
	names := make(map[string]bool)
	for _, n := range cycles[0] {
		names[n] = true
	}
	if !names["cycle-a"] || !names["cycle-b"] {
		t.Errorf("cycle should contain both cycle-a and cycle-b, got %v", cycles[0])
	}
}

func loadFixture(path string) (*formula.Formula, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return formula.Parse(data)
}
