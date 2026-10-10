package domain

import (
	"strings"
	"testing"
)

func TestValidatePrice(t *testing.T) {
	// Prices are in kobo, so ₦6,500 is 650,000.
	if err := ValidatePrice(650_000); err != nil {
		t.Errorf("ValidatePrice(650000) returned error: %v", err)
	}

	for _, price := range []int64{0, -1, 20_000_000_000} {
		if err := ValidatePrice(price); err == nil {
			t.Errorf("ValidatePrice(%d) accepted an invalid amount", price)
		}
	}
}

func TestValidateName(t *testing.T) {
	got, err := ValidateName("  Broiler Chicken  ")
	if err != nil {
		t.Fatalf("ValidateName() returned error: %v", err)
	}
	if got != "Broiler Chicken" {
		t.Errorf("ValidateName() = %q, want the trimmed name", got)
	}

	if _, err := ValidateName("   "); err == nil {
		t.Error("ValidateName() accepted a blank name")
	}
	if _, err := ValidateName(strings.Repeat("a", 121)); err == nil {
		t.Error("ValidateName() accepted an over-long name")
	}
}

func TestValidateUnit(t *testing.T) {
	for _, unit := range []string{"bird", "head", "bag", "100kg bag", "50kg bag"} {
		if _, err := ValidateUnit(unit); err != nil {
			t.Errorf("ValidateUnit(%q) returned error: %v", unit, err)
		}
	}

	if _, err := ValidateUnit(""); err == nil {
		t.Error("ValidateUnit() accepted a blank unit")
	}
}

func TestValidateStatus(t *testing.T) {
	for _, status := range []string{StatusActive, StatusArchived} {
		if _, err := ValidateStatus(status); err != nil {
			t.Errorf("ValidateStatus(%q) returned error: %v", status, err)
		}
	}

	for _, status := range []string{"", "published", "deleted"} {
		if _, err := ValidateStatus(status); err == nil {
			t.Errorf("ValidateStatus(%q) accepted an unknown status", status)
		}
	}
}

func TestNormalizeSort(t *testing.T) {
	cases := map[string]string{
		SortPriceAsc:  SortPriceAsc,
		SortPriceDesc: SortPriceDesc,
		SortName:      SortName,
		SortNewest:    SortNewest,
		"":            SortFeatured,
		"nonsense":    SortFeatured,
	}

	for input, want := range cases {
		if got := NormalizeSort(input); got != want {
			t.Errorf("NormalizeSort(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestIsPurchasableAndPrimaryImage(t *testing.T) {
	active := &Product{Status: StatusActive, Images: []Image{{URL: "/images/broiler.jpg"}}}
	if !active.IsPurchasable() {
		t.Error("an active product should be purchasable")
	}
	if got := active.PrimaryImage(); got != "/images/broiler.jpg" {
		t.Errorf("PrimaryImage() = %q, want the first image", got)
	}

	archived := &Product{Status: StatusArchived}
	if archived.IsPurchasable() {
		t.Error("an archived product must not be purchasable")
	}
	if got := archived.PrimaryImage(); got != "" {
		t.Errorf("PrimaryImage() = %q, want an empty string with no images", got)
	}
}
