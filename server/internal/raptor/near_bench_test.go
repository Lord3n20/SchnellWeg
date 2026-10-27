package raptor

import (
	"os"
	"testing"
	"time"
)

func BenchmarkNear(b *testing.B) {
	d := os.Getenv("RAW")
	if d == "" {
		b.Skip()
	}
	tt, err := Load([]string{d + "/sr_gtfs.zip", d + "/de_rv.zip", d + "/de_fv.zip:fern"}, time.Date(2026, 10, 27, 0, 0, 0, 0, time.Local), 400)
	if err != nil {
		b.Fatal(err)
	}
	b.Run("Near", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			tt.Near(50.7677, 6.0912, 800)
		}
	})
	acc := tt.Near(50.7677, 6.0912, 800)
	b.Logf("%d Halte im Umkreis", len(acc))
	b.Run("PerRoute", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			tt.PerRoute(acc)
		}
	})
}
