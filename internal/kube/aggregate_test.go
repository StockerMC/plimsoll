package kube

import (
	"testing"
	"time"

	"github.com/StockerMC/plimsoll/internal/recommender"
)

type point struct {
	pod   string
	bytes float64
	at    time.Duration
}

func TestAggregator(t *testing.T) {
	t0 := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	// One pod rising by 1 MiB every 15 s for ten minutes, then one more sample.
	var ramp []point
	for i := 0; i <= 40; i++ {
		ramp = append(ramp, point{"a", float64((i + 1) << 20), time.Duration(i) * 15 * time.Second})
	}
	tests := []struct {
		name   string
		points []point
		want   []recommender.Sample
	}{
		{
			name:   "15 s samples over two windows give two peaks",
			points: ramp,
			want: []recommender.Sample{
				{Time: t0, Peaks: []recommender.InstancePeak{{Instance: "a", Peak: 20 << 20}}},
				{Time: t0.Add(5 * time.Minute), Peaks: []recommender.InstancePeak{{Instance: "a", Peak: 40 << 20}}},
			},
		},
		{
			name: "two pods get separate peaks",
			points: []point{
				{"a", 3, 0}, {"b", 7, 0}, {"a", 5, 15 * time.Second}, {"b", 2, 15 * time.Second}, {"a", 1, 5 * time.Minute},
			},
			want: []recommender.Sample{
				{Time: t0, Peaks: []recommender.InstancePeak{{Instance: "a", Peak: 5}, {Instance: "b", Peak: 7}}},
			},
		},
		{
			name:   "a sample behind the open window counts towards it",
			points: []point{{"a", 3, 5 * time.Minute}, {"b", 9, 4*time.Minute + 59*time.Second}, {"a", 1, 10 * time.Minute}},
			want: []recommender.Sample{
				{Time: t0.Add(5 * time.Minute), Peaks: []recommender.InstancePeak{{Instance: "a", Peak: 3}, {Instance: "b", Peak: 9}}},
			},
		},
		{
			name:   "nothing closes inside the first window",
			points: []point{{"a", 3, 0}, {"a", 4, time.Minute}},
			want:   nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := NewAggregator(5 * time.Minute)
			var got []recommender.Sample
			for _, p := range tt.points {
				if s, ok := a.Add(p.pod, p.bytes, t0.Add(p.at)); ok {
					got = append(got, s)
				}
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %d windows %+v, want %d", len(got), got, len(tt.want))
			}
			for i := range got {
				if !got[i].Time.Equal(tt.want[i].Time) {
					t.Errorf("window %d at %v, want %v", i, got[i].Time, tt.want[i].Time)
				}
				if len(got[i].Peaks) != len(tt.want[i].Peaks) {
					t.Fatalf("window %d peaks %+v, want %+v", i, got[i].Peaks, tt.want[i].Peaks)
				}
				for j := range got[i].Peaks {
					if got[i].Peaks[j] != tt.want[i].Peaks[j] {
						t.Errorf("window %d peak %d = %+v, want %+v", i, j, got[i].Peaks[j], tt.want[i].Peaks[j])
					}
				}
			}
		})
	}
}
