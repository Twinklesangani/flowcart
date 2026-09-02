package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	argonVersion     uint32 = 19
	argonMemory      uint32 = 19 * 1024
	argonIterations  uint32 = 2
	argonParallelism uint8  = 1
	saltLength       uint32 = 16
	hashLength       uint32 = 32
)

func HashPassword(password string) (string, error) {
	salt := make([]byte, saltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}

	hash := argon2.IDKey([]byte(password), salt, argonIterations, argonMemory, argonParallelism, hashLength)
	encode := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argonVersion, argonMemory, argonIterations, argonParallelism,
		encode.EncodeToString(salt), encode.EncodeToString(hash)), nil
}

func VerifyPassword(password, encodedHash string) bool {
	parts := strings.Split(encodedHash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" {
		return false
	}

	parameters := map[string]uint32{}
	for _, parameter := range strings.Split(parts[3], ",") {
		values := strings.SplitN(parameter, "=", 2)
		if len(values) != 2 {
			return false
		}
		value, err := strconv.ParseUint(values[1], 10, 32)
		if err != nil {
			return false
		}
		parameters[values[0]] = uint32(value)
	}
	memory, okMemory := parameters["m"]
	iterations, okIterations := parameters["t"]
	parallelism, okParallelism := parameters["p"]
	if !okMemory || !okIterations || !okParallelism || memory == 0 || iterations == 0 || parallelism == 0 {
		return false
	}

	decode := base64.RawStdEncoding
	salt, err := decode.DecodeString(parts[4])
	if err != nil {
		return false
	}
	expected, err := decode.DecodeString(parts[5])
	if err != nil || len(expected) == 0 {
		return false
	}
	actual := argon2.IDKey([]byte(password), salt, iterations, memory, uint8(parallelism), uint32(len(expected)))
	return subtle.ConstantTimeCompare(actual, expected) == 1
}
