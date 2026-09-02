package auth

import "testing"

func TestPasswordHashAndVerify(t *testing.T) {
	hashed, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	if hashed == "correct horse battery staple" {
		t.Fatal("password was stored in plaintext")
	}
	if !VerifyPassword("correct horse battery staple", hashed) {
		t.Fatal("VerifyPassword() rejected the correct password")
	}
	if VerifyPassword("wrong password", hashed) {
		t.Fatal("VerifyPassword() accepted the wrong password")
	}
}

func TestPasswordHashesUseDifferentSalts(t *testing.T) {
	first, err := HashPassword("same password")
	if err != nil {
		t.Fatal(err)
	}
	second, err := HashPassword("same password")
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("password hashes reused the same salt")
	}
}
