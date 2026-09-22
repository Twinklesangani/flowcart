package inventory

import (
	"os"
	"strings"
	"testing"
)

func TestLowStockDonorQueryPreselectsBeforeReservationAggregation(t *testing.T) {
	data, err := os.ReadFile("repository.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	for _, fragment := range []string{"CROSS JOIN LATERAL", "LIMIT $3", "candidate_ids AS", "bounded AS"} {
		if !strings.Contains(source, fragment) {
			t.Fatalf("donor query is missing bounded preselection fragment %q", fragment)
		}
	}
	donorQuery := source[strings.Index(source, "WITH candidate_ids AS"):]
	if strings.Index(donorQuery, "CROSS JOIN LATERAL") > strings.Index(donorQuery, "LEFT JOIN inventory_reservations r") {
		t.Fatal("reservation aggregation appears before donor candidate preselection")
	}
}
