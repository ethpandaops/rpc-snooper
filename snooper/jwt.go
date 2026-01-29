package snooper

import (
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/sirupsen/logrus"
)

// ParseJWTSecret parses a JWT secret from either a file path or hex-encoded string.
// If the value looks like a file path, it reads the secret from the file.
// Otherwise, it treats it as a hex-encoded value (with optional "0x" prefix).
func ParseJWTSecret(s string, log logrus.FieldLogger) []byte {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}

	// Check if it looks like a file path
	if strings.HasPrefix(s, "/") || strings.HasPrefix(s, "./") || strings.HasPrefix(s, "../") {
		data, err := os.ReadFile(s)
		if err != nil {
			log.WithError(err).WithField("path", s).Error("failed to read JWT secret from file")

			return nil
		}

		// File contents should be hex-encoded
		return parseHexSecret(strings.TrimSpace(string(data)))
	}

	return parseHexSecret(s)
}

// parseHexSecret parses a hex-encoded secret string.
func parseHexSecret(s string) []byte {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "0x")

	secret, err := hex.DecodeString(s)
	if err != nil {
		return nil
	}

	return secret
}

// CreateJWTToken creates a JWT token for Engine API authentication.
func CreateJWTToken(secret []byte) (string, error) {
	if len(secret) == 0 {
		return "", fmt.Errorf("no JWT secret configured")
	}

	now := time.Now()
	claims := jwt.MapClaims{
		"iat": now.Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	return token.SignedString(secret)
}
