// Package credential contains the dependency-neutral API-key credential
// primitives shared by request authentication and transactional authority
// proof verification. It performs no database access and never logs proofs.
package credential

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

const (
	DigestFamilyPrefix = "sha256:"
	DigestPrefix       = "sha256:v1:"
	BcryptHashLength   = 60
)

// Digest returns the versioned digest used for fast verification of
// server-generated, high-entropy API keys.
func Digest(proof string) string {
	digest := sha256.Sum256([]byte(proof))
	return DigestPrefix + hex.EncodeToString(digest[:])
}

func verifyDigest(storedDigest, proof string) error {
	if !strings.HasPrefix(storedDigest, DigestPrefix) {
		return fmt.Errorf("unsupported API key digest version")
	}
	encodedDigest := storedDigest[len(DigestPrefix):]
	if len(encodedDigest) != hex.EncodedLen(sha256.Size) {
		return fmt.Errorf("invalid API key digest encoding")
	}
	var expected [sha256.Size]byte
	if _, err := hex.Decode(expected[:], []byte(encodedDigest)); err != nil {
		return fmt.Errorf("invalid API key digest encoding")
	}
	candidate := sha256.Sum256([]byte(proof))
	if subtle.ConstantTimeCompare(expected[:], candidate[:]) != 1 {
		return fmt.Errorf("API key digest mismatch")
	}
	return nil
}

// Verify validates proof against a stored raw digest, composite credential, or
// legacy bcrypt credential. A replacement is returned only after a legacy
// bcrypt credential verifies successfully, preserving rollback compatibility.
func Verify(storedHash, proof string) (replacementHash string, err error) {
	if strings.HasPrefix(storedHash, DigestFamilyPrefix) {
		return "", verifyDigest(storedHash, proof)
	}

	if len(storedHash) > BcryptHashLength {
		bcryptHash := storedHash[:BcryptHashLength]
		if _, err := bcrypt.Cost([]byte(bcryptHash)); err != nil {
			return "", fmt.Errorf("invalid composite API key credential: %w", err)
		}
		digest := storedHash[BcryptHashLength:]
		if !strings.HasPrefix(digest, DigestFamilyPrefix) {
			return "", fmt.Errorf("invalid composite API key credential")
		}
		return "", verifyDigest(digest, proof)
	}

	if err := bcrypt.CompareHashAndPassword([]byte(storedHash), []byte(proof)); err != nil {
		return "", err
	}
	return storedHash + Digest(proof), nil
}
