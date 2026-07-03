package authn

// ResetCredentialPrefix is the non-secret prefix every raw password-reset
// credential starts with.
const ResetCredentialPrefix = "rst_"

// GenerateResetCredential creates a new opaque password-reset credential
// using the same lookup-ID-plus-secret shape as an API token, so it can be
// found in O(1) time before a constant-time digest comparison.
func GenerateResetCredential() (raw, lookupID string, secret []byte, err error) {
	return generateLookupSecretPair(ResetCredentialPrefix)
}

// ParseResetCredential splits a presented reset credential into its lookup
// ID and secret.
func ParseResetCredential(raw string) (lookupID string, secret []byte, err error) {
	return parseLookupSecretPair(raw, ResetCredentialPrefix)
}
