package draft

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// PromptsVersion fingerprints every rule and schema Claude is given, the
// model and the settings isolation. It changes exactly when what Claude is
// told or runs as changes, committed or not, so usage can be compared
// across prompt versions.
func PromptsVersion() string {
	h := sha256.New()
	for _, part := range []string{
		strings.Join(isolation, " "),
		triageRules, triageSchema,
		briefRules, briefSchema,
		questionRules, questionsSchema, plainText,
		stampRules, stampSchema,
		checkRules, checkSchema,
	} {
		h.Write([]byte(part))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:10]
}
