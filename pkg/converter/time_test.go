package converter_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/viettechno/vietgara-backend/pkg/converter"
)

func TestTimeToProto(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input time.Time
		want  *timestamppb.Timestamp
	}{
		{
			name:  "zero time",
			input: time.Time{},
			want:  nil,
		},
		{
			name:  "non-zero time",
			input: time.Date(2024, time.January, 2, 3, 4, 5, 678900000, time.FixedZone("UTC+7", 7*60*60)),
			want:  timestamppb.New(time.Date(2024, time.January, 2, 3, 4, 5, 678900000, time.FixedZone("UTC+7", 7*60*60))),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := converter.TimeToProto(tt.input)

			if tt.want == nil {
				require.Nil(t, got)
				return
			}

			require.True(t, proto.Equal(tt.want, got))
		})
	}
}
