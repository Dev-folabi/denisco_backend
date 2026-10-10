package domain

import "testing"

func TestBadgeThresholds(t *testing.T) {
	// The thresholds come from the prototype's stock badges.
	cases := map[int]string{
		-5:  BadgeOutOfStock,
		0:   BadgeOutOfStock,
		1:   BadgeLowStock,
		10:  BadgeLowStock,
		11:  BadgeInStock,
		350: BadgeInStock,
	}

	for sellable, want := range cases {
		if got := Badge(sellable); got != want {
			t.Errorf("Badge(%d) = %q, want %q", sellable, got, want)
		}
	}
}

func TestSellable(t *testing.T) {
	cases := []struct {
		name      string
		available int
		reserved  int
		want      int
	}{
		{"nothing reserved", 100, 0, 100},
		{"partly reserved", 100, 40, 60},
		{"fully reserved", 5, 5, 0},
		{"never negative", 5, 9, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			record := &Inventory{AvailableQty: tc.available, ReservedQty: tc.reserved}
			if got := record.Sellable(); got != tc.want {
				t.Errorf("Sellable() = %d, want %d", got, tc.want)
			}
		})
	}
}
