package jointspace

import "encoding/base64"

// philipsSecret is the publicly-known HMAC signing secret required to
// participate in the JointSpace pair-grant protocol — not a project credential.
// It originated from reverse-engineering shared across pylips,
// bcyran/philipstv, jointspace-cli, and others.
const philipsSecretB64 = "JCqdN5AcnAHgJYseUn7ER5k3qgtemfUvMRghQpTfTZq7Cvv8EPQPqfz6dDxPQPSu4gKFPWkJGw32zyASgJkHwCjU"

var philipsSecret = mustDecodeSecret(philipsSecretB64)

func mustDecodeSecret(s string) []byte {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		panic("jointspace: cannot decode embedded philips secret: " + err.Error())
	}
	return b
}
