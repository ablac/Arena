package accounts

import (
	"context"
	"testing"
)

func TestIncludedArenaDoesNotGrantPaidCosmetics(t *testing.T) {
	for _, tc := range []struct {
		name, row string
		want      bool
	}{
		{"included base", `{"source":"included","productSlug":"arena","active":true,"planSlug":null,"upgrade":null}`, false},
		{"missing upgrade", `{"source":"included","productSlug":"arena","active":true}`, false},
		{"paid upgrade", `{"source":"included","productSlug":"arena","active":true,"upgrade":{"source":"subscription","productSlug":"arena","planSlug":"all-access","active":true}}`, true},
		{"expired upgrade", `{"source":"included","productSlug":"arena","active":true,"upgrade":{"source":"subscription","productSlug":"arena","planSlug":"all-access","active":false}}`, false},
		{"other product", `{"source":"included","productSlug":"arena","active":true,"upgrade":{"source":"subscription","productSlug":"kynetik","planSlug":"pro","active":true}}`, false},
		{"nested included", `{"source":"included","productSlug":"arena","active":true,"upgrade":{"source":"included","productSlug":"arena","planSlug":"all-access","active":true}}`, false},
		{"missing upgrade plan", `{"source":"included","productSlug":"arena","active":true,"upgrade":{"source":"subscription","productSlug":"arena","active":true}}`, false},
		{"unknown source", `{"source":"unknown","productSlug":"arena","active":true}`, false},
		{"legacy paid", `{"productSlug":"arena","active":true}`, true},
		{"explicit paid", `{"source":"subscription","productSlug":"arena","active":true}`, true},
		{"legacy lapsed", `{"productSlug":"arena","active":false}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := serving(t, 200, `{"entitlements":[`+tc.row+`]}`, nil)
			snapshot, err := client.Fetch(context.Background(), "transient-token")
			if err != nil {
				t.Fatal(err)
			}
			if got := snapshot.ArenaSubscriptionActive(); got != tc.want {
				t.Fatalf("cosmetics grant=%v want%v", got, tc.want)
			}
		})
	}
}
