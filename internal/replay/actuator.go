package replay

import (
	"math"
	"time"

	"github.com/StockerMC/plimsoll/internal/recommender"
)

// Actuator turns a recommendation into the limit a container would run
// with.
type Actuator interface {
	Limit(r recommender.Recommendation, at time.Time) float64
}

// Every sets the limit to the target whenever it changes.
func Every(float64, time.Time) Actuator {
	return every{}
}

type every struct{}

func (every) Limit(r recommender.Recommendation, _ time.Time) float64 {
	return r.Target
}

// Upstream updater defaults: --pod-update-threshold and
// --in-recommendation-bounds-eviction-lifetime-threshold.
const (
	updateThreshold = 0.10
	updateLifetime  = 12 * time.Hour
)

// Updater applies upstream VPA's updater rule: the request changes when it
// is outside the recommendation's bounds, or when it differs from the target
// by at least 10% and the last change is at least 12 h old. A recommender
// whose bounds equal its target would fail the bounds test at every drift,
// so for it only the second rule applies.
func Updater(request float64, at time.Time) Actuator {
	return &updater{request: request, changed: at}
}

type updater struct {
	request float64
	changed time.Time
}

func (u *updater) Limit(r recommender.Recommendation, at time.Time) float64 {
	if r.Target <= 0 {
		return u.request
	}
	outside := r.Lower < r.Upper && (u.request < r.Lower || u.request > r.Upper)
	drift := u.request == 0 || math.Abs(r.Target-u.request)/u.request >= updateThreshold
	if outside || (drift && at.Sub(u.changed) >= updateLifetime) {
		u.request = r.Target
		u.changed = at
	}
	return u.request
}
