package auth

import (
	"strings"
	"testing"
)

func TestHashAndCheckPassword(t *testing.T) {
	pw := "Sup3rSecret!"
	hash, err := HashPassword(pw)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if hash == pw {
		t.Fatal("hash must not equal plaintext")
	}
	if !strings.HasPrefix(hash, "$2") {
		t.Errorf("expected bcrypt hash prefix, got %q", hash)
	}

	if err := CheckPassword(pw, hash); err != nil {
		t.Errorf("correct password should verify: %v", err)
	}
	if err := CheckPassword("wrong", hash); err == nil {
		t.Error("wrong password should not verify")
	}
}

func TestHashPassword_Salted(t *testing.T) {
	// bcrypt salts each hash, so the same password hashes differently each time.
	h1, _ := HashPassword("samePassword1!")
	h2, _ := HashPassword("samePassword1!")
	if h1 == h2 {
		t.Fatal("expected distinct hashes for the same password (salting)")
	}
}

func TestCheckPassword_ErrorDoesNotLeakSecret(t *testing.T) {
	hash, _ := HashPassword("Sup3rSecret!")
	err := CheckPassword("attackerGuess123", hash)
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "attackerGuess123") {
		t.Errorf("error message leaked the attempted password: %v", err)
	}
}

func TestValidatePasswordStrength(t *testing.T) {
	tests := []struct {
		name    string
		pw      string
		wantErr bool
	}{
		{"strong", "Str0ng!Pass", false},
		{"too short", "Ab1!", true},
		{"no upper", "str0ng!pass", true},
		{"no lower", "STR0NG!PASS", true},
		{"no number", "Strong!Pass", true},
		{"no special", "Str0ngPass1", true},
		{"common password", "Password123", true},
		{"common with special", "Admin123!", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePasswordStrength(tt.pw)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidatePasswordStrength(%q) err=%v, wantErr=%v", tt.pw, err, tt.wantErr)
			}
		})
	}
}

func TestValidateEmail(t *testing.T) {
	valid := []string{
		"alice@example.com",
		"bob.smith@sub.domain.co",
		"a+tag@example.io",
		"user_name@example-host.com",
	}
	for _, e := range valid {
		if err := ValidateEmail(e); err != nil {
			t.Errorf("expected %q to be valid: %v", e, err)
		}
	}

	invalid := []string{
		"",
		"no-at-sign",
		"@example.com",
		"user@",
		"user@nodot",
		"user@domain.c", // TLD too short
		"user name@example.com",
	}
	for _, e := range invalid {
		if err := ValidateEmail(e); err == nil {
			t.Errorf("expected %q to be invalid", e)
		}
	}
}
