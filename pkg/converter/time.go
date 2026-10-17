package converter

import (
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"
)

// TimeToProto converts a time.Time to a *timestamppb.Timestamp. If the input time is zero, it returns nil.
func TimeToProto(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}

	return timestamppb.New(t)
}

// TimePtrToProto converts t, or returns nil when t is nil.
func TimePtrToProto(t *time.Time) *timestamppb.Timestamp {
	if t == nil {
		return nil
	}

	return TimeToProto(*t)
}
