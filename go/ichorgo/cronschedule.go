package ichorgo

import (
	"time"

	sharedcron "github.com/cyrinux/ichor/go/internal/cronexpr"
)

// cronSchedule preserves the local interface used by cluster schedule readers.
type cronSchedule struct{ sharedcron.Schedule }

func parseCronSchedule(spec string) (cronSchedule, error) {
	schedule, err := sharedcron.Parse(spec)
	return cronSchedule{schedule}, err
}

func (s cronSchedule) next(from time.Time) time.Time { return s.Next(from) }
