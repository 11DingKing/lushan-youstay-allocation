package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"strings"
)

const hashPrefix = "sha256:"

func HashPassword(password string) string {
	digest := sha256.Sum256([]byte(password))
	return hashPrefix + hex.EncodeToString(digest[:])
}

func CheckPassword(encoded, password string) bool {
	if !strings.HasPrefix(encoded, hashPrefix) {
		return false
	}
	expected := HashPassword(password)
	return subtle.ConstantTimeCompare([]byte(encoded), []byte(expected)) == 1
}
