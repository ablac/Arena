package demobots

import "testing"

func TestDemoBotActsOnlyOnAliveTick(t *testing.T) {
	for _, tc := range []struct {
		name string
		msg  map[string]interface{}
		want bool
	}{
		{
			name: "alive",
			msg:  map[string]interface{}{"your_state": map[string]interface{}{"is_alive": true}},
			want: true,
		},
		{
			name: "dead",
			msg:  map[string]interface{}{"your_state": map[string]interface{}{"is_alive": false}},
			want: false,
		},
		{name: "missing state", msg: map[string]interface{}{}, want: false},
		{name: "malformed state", msg: map[string]interface{}{"your_state": "alive"}, want: false},
		{
			name: "missing alive flag",
			msg:  map[string]interface{}{"your_state": map[string]interface{}{}},
			want: false,
		},
		{
			name: "malformed alive flag",
			msg:  map[string]interface{}{"your_state": map[string]interface{}{"is_alive": 1}},
			want: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldActOnTick(tc.msg); got != tc.want {
				t.Fatalf("shouldActOnTick() = %v, want %v", got, tc.want)
			}
		})
	}
}
