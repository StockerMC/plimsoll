package main

import (
	"fmt"
	"io"

	"github.com/StockerMC/plimsoll/internal/replay"
	"github.com/StockerMC/plimsoll/internal/trace"
)

func inspectCollection(r *trace.Reader, id int64) error {
	for {
		c, err := r.Next()
		if err == io.EOF {
			return fmt.Errorf("%w: %d", errNotFound, id)
		}
		if err != nil {
			return err
		}
		if c.ID != id {
			continue
		}
		steps := trace.Steps(c)
		instances := make(map[int]bool)
		for _, w := range c.Windows {
			instances[w.Instance] = true
		}
		// Updates of several instances at one time that disagree on the
		// new value.
		times, disagree := 0, 0
		for i := 0; i < len(c.Updates); {
			j := i + 1
			for j < len(c.Updates) && c.Updates[j].Time.Equal(c.Updates[i].Time) {
				j++
			}
			times++
			for _, u := range c.Updates[i+1 : j] {
				if u.ReqMem != c.Updates[i].ReqMem {
					disagree++
					break
				}
			}
			i = j
		}
		fmt.Printf("collection %d: vertical_scaling %d, split %s, %d rows (%d skipped), %d windows, %d instances, %d updates at %d times (%d with disagreeing instances)\n",
			c.ID, c.VerticalScaling, replay.Split(c), len(c.Windows), c.Skipped, len(steps), len(instances), len(c.Updates), times, disagree)
		if len(steps) > 0 {
			fmt.Printf("windows from %d to %d us, trace days %d to %d\n", steps[0].Time.UnixMicro(), steps[len(steps)-1].Time.UnixMicro(), steps[0].Day, steps[len(steps)-1].Day)
		}
		was := make(map[int]float64)
		j := 0
		for _, u := range c.Updates[:min(3, len(c.Updates))] {
			for ; j < len(c.Requests) && c.Requests[j].Time.Before(u.Time); j++ {
				was[c.Requests[j].Instance] = c.Requests[j].ReqMem
			}
			fmt.Printf("update at %d us: instance %d request %g (was %g)\n", u.Time.UnixMicro(), u.Instance, u.ReqMem, was[u.Instance])
			was[u.Instance] = u.ReqMem
		}
		return nil
	}
}
