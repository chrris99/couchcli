package philips

// The Philips HMAC signing secret. This is a publicly-known constant required
// to participate in the JointSpace pair-grant protocol; it is not a
// project-specific credential. It originated from reverse-engineering work
// shared across pylips, bcyran/philipstv, jointspace-cli, and others.

import (
	"encoding/base64"
)

const philipsSecretB64 = "JCqdN5AcnAHgJYseUn7ER5k3qgtemfUvMRghQpTfTZq7Cvv8EPQPqfz6dDxPQPSu4gKFPWkJGw32zyASgJkHwCjU"

var philipsSecret = mustDecodeSecret(philipsSecretB64)

func mustDecodeSecret(s string) []byte {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		panic("jointspace: cannot decode embedded philips secret: " + err.Error())
	}
	return b
}
