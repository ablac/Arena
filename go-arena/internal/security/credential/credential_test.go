package credential

import (
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestVerifySupportsCurrentAndLegacyCredentials(t *testing.T) {
	const proof = "arena_control_proof_1234567890"
	legacy, err := bcrypt.GenerateFromPassword([]byte(proof), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("generate legacy credential: %v", err)
	}

	replacement, err := Verify(string(legacy), proof)
	if err != nil {
		t.Fatalf("verify legacy credential: %v", err)
	}
	if !strings.HasPrefix(replacement, string(legacy)+DigestPrefix) {
		t.Fatalf("legacy replacement = %q, want rollback bcrypt plus versioned digest", replacement)
	}
	if secondReplacement, err := Verify(replacement, proof); err != nil || secondReplacement != "" {
		t.Fatalf("verify composite = (%q, %v), want no replacement", secondReplacement, err)
	}
	if digestReplacement, err := Verify(Digest(proof), proof); err != nil || digestReplacement != "" {
		t.Fatalf("verify digest = (%q, %v), want no replacement", digestReplacement, err)
	}
}

func TestVerifyRejectsWrongProofAndUnknownDigestVersions(t *testing.T) {
	const proof = "arena_control_proof_1234567890"
	for _, stored := range []string{
		Digest(proof),
		"sha256:v2:" + strings.Repeat("0", 64),
	} {
		if _, err := Verify(stored, "arena_wrong_control_proof_1234"); err == nil {
			t.Fatalf("Verify(%q) accepted invalid proof", stored)
		}
	}
}
