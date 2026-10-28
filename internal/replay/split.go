package replay

import (
	"hash/fnv"
	"strconv"

	"github.com/StockerMC/plimsoll/internal/trace"
)

// Group is the unit of the tune/report split and of the bootstrap: the
// collection's user and logical name, so collections of one logical job
// stay together, or its id when either is missing.
func Group(c trace.Collection) string {
	if c.User == "" || c.LogicalName == "" {
		return strconv.FormatInt(c.ID, 10)
	}
	return c.User + "/" + c.LogicalName
}

// Split assigns the collection's group to "tune" or "report" by the low bit
// of its FNV-1a hash.
func Split(c trace.Collection) string {
	h := fnv.New64a()
	h.Write([]byte(Group(c)))
	if h.Sum64()&1 == 0 {
		return "tune"
	}
	return "report"
}
