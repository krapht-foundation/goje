package skirmishbench

import "testing"

// Issuing a round of group orders must not allocate once the squads exist; the
// allocations the benchmark reports in the orders mode come from the collision
// buckets growing as units reach new tiles, not from the orders.
func TestIssuingGroupOrdersDoesNotAllocate(t *testing.T) {
	scene, err := NewOrders()
	if err != nil {
		t.Fatal(err)
	}
	for range 300 {
		if _, err := scene.Step(); err != nil {
			t.Fatal(err)
		}
	}
	if allocs := testing.AllocsPerRun(20, scene.issue); allocs != 0 {
		t.Errorf("issue allocated %v times per round, want 0", allocs)
	}
}
