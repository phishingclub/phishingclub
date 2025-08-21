package random

import "testing"

func TestGenerateRandomLowerUpperAndNumeric(t *testing.T) {
	t.Run("should generate a random password of expected length", func(t *testing.T) {
		length := 10
		password, err := GenerateRandomURLBase64Encoded(length)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if len(password) != length {
			t.Errorf("expected password length to be %d, got %d", length, len(password))
		}
	})

}
