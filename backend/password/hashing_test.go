package password_test

import (
	"testing"

	"github.com/phishingclub/phishingclub/password"
)

func TestArgon2HasherAndVerifier(t *testing.T) {
	// Create a new hasher with default values
	hasher := password.NewHasherWithDefaultValues()

	// Test password hashing
	pass := "mysecretpassword"
	hash, err := hasher.Hash(pass)
	if err != nil {
		t.Errorf("Failed to hash password: %v", err)
	}

	// Create a new verifier
	verifier := password.NewArgon2Verifier()

	// Test password verification
	match, err := verifier.Verify(pass, hash)
	if err != nil {
		t.Errorf("Failed to verify password: %v", err)
	}
	if !match {
		t.Error("Password verification failed")
	}

	// Test incorrect password verification
	incorrectPassword := "incorrectpassword"
	match, err = verifier.Verify(incorrectPassword, hash)
	if err != nil {
		t.Errorf("Failed to verify password: %v", err)
	}
	if match {
		t.Error("Incorrect password should not match")
	}
}
