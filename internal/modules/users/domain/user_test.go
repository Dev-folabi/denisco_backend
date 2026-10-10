package domain

import "testing"

func TestValidateEmailNormalizes(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"lowercases", "Demo@Denisco.COM", "demo@denisco.com"},
		{"trims", "  demo@denisco.com  ", "demo@denisco.com"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ValidateEmail(tc.input)
			if err != nil {
				t.Fatalf("ValidateEmail(%q) returned error: %v", tc.input, err)
			}
			if got != tc.want {
				t.Errorf("ValidateEmail(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestValidateEmailRejectsInvalid(t *testing.T) {
	for _, input := range []string{"", "   ", "not-an-email", "a@b", "a@@b.com"} {
		if _, err := ValidateEmail(input); err == nil {
			t.Errorf("ValidateEmail(%q) accepted an invalid address", input)
		}
	}
}

func TestValidatePhone(t *testing.T) {
	got, err := ValidatePhone("+234 801 234 5678")
	if err != nil {
		t.Fatalf("ValidatePhone() returned error: %v", err)
	}
	if got != "+2348012345678" {
		t.Errorf("ValidatePhone() = %q, want %q", got, "+2348012345678")
	}

	for _, input := range []string{"", "12345", "0123456789012345678"} {
		if _, err := ValidatePhone(input); err == nil {
			t.Errorf("ValidatePhone(%q) accepted an invalid number", input)
		}
	}
}

func TestValidateName(t *testing.T) {
	got, err := ValidateName("first_name", "  Ada  ")
	if err != nil {
		t.Fatalf("ValidateName() returned error: %v", err)
	}
	if got != "Ada" {
		t.Errorf("ValidateName() = %q, want %q", got, "Ada")
	}

	if _, err := ValidateName("first_name", "   "); err == nil {
		t.Error("ValidateName() accepted a blank name")
	}
}

func TestEnsureActive(t *testing.T) {
	cases := map[string]bool{
		StatusActive:    true,
		StatusInactive:  false,
		StatusSuspended: false,
	}

	for status, wantAllowed := range cases {
		user := &User{Status: status}
		err := user.EnsureActive()
		if wantAllowed && err != nil {
			t.Errorf("EnsureActive() for status %q returned error: %v", status, err)
		}
		if !wantAllowed && err == nil {
			t.Errorf("EnsureActive() allowed status %q", status)
		}
	}
}

func TestIsAdmin(t *testing.T) {
	cases := map[string]bool{
		RoleCustomer:   false,
		RoleAdmin:      true,
		RoleSuperAdmin: true,
	}

	for role, want := range cases {
		user := &User{Role: role}
		if got := user.IsAdmin(); got != want {
			t.Errorf("IsAdmin() for role %q = %v, want %v", role, got, want)
		}
	}
}

func TestFullName(t *testing.T) {
	user := &User{FirstName: "Ada", LastName: "Obi"}
	if got := user.FullName(); got != "Ada Obi" {
		t.Errorf("FullName() = %q, want %q", got, "Ada Obi")
	}

	single := &User{FirstName: "Ada"}
	if got := single.FullName(); got != "Ada" {
		t.Errorf("FullName() = %q, want %q", got, "Ada")
	}
}
