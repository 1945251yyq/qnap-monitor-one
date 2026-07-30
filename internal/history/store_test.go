package history

import (
	"testing"
	"time"

	"github.com/1945251yyq/qnap-status-lite/internal/monitor"
)

func TestRoundTrip(t *testing.T) {
	store, err := Open(t.TempDir(), 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().Truncate(time.Second)
	if err := store.Add(monitor.Snapshot{
		Timestamp: now,
		System:    monitor.System{CPUPercent: 12.5, MemoryPercent: 40},
		Networks:  []monitor.Network{{RxPerSec: 10, TxPerSec: 20}},
		Volumes:   []monitor.Volume{{Percent: 55}},
	}); err != nil {
		t.Fatal(err)
	}
	points, err := store.Query(now.Add(-time.Second), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(points) != 1 || points[0].CPU != 12.5 || points[0].NetworkTx != 20 || points[0].VolumePercent != 55 {
		t.Fatalf("unexpected history: %#v", points)
	}
}
