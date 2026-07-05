package philips

import (
	"context"
	"runtime"

	"github.com/chrris99/couchcli/internal/clients/jointspace"
)

// PinPrompter collects the pairing PIN from the user, within the pair window.
type PinPrompter = jointspace.PinPrompter

// Pairing error sentinels, re-exported so callers match with errors.Is without
// importing the protocol client.
var (
	ErrPINRejected    = jointspace.ErrPINRejected
	ErrPairingRefused = jointspace.ErrPairingRefused
	ErrTimeout        = jointspace.ErrTimeout
	ErrNotPairable    = jointspace.ErrNotPairable
)

// Profile is what probing a TV learned: which transport answered plus the
// system metadata needed to build a registry entry and decide on pairing.
type Profile struct {
	Address         string
	Secure          bool
	Port            int
	Name            string
	Model           string
	SerialNumber    string
	SoftwareVersion string
	APIVersion      int
	PairingRequired bool
}

// Detect races HTTPS:1926 and HTTP:1925 at addr and reports the working
// transport plus whether the TV requires pairing.
func Detect(ctx context.Context, addr string) (Profile, error) {
	client, sys, err := jointspace.DetectEndpoint(ctx, addr)
	if err != nil {
		return Profile{}, err
	}
	return Profile{
		Address:         addr,
		Secure:          client.Secure(),
		Port:            client.Port(),
		Name:            sys.Name,
		Model:           sys.Model,
		SerialNumber:    sys.SerialNumber,
		SoftwareVersion: sys.SoftwareVersion,
		APIVersion:      sys.APIVersionMajor,
		PairingRequired: client.Pair.Required(sys),
	}, nil
}

// Pair runs one full pairing handshake against the secure endpoint at addr,
// prompting for the PIN via prompt. It uses a fresh client so a rejected PIN
// can't poison digest state for a retry. clientName appears in the TV's
// paired-devices list. Returns the durable digest credentials.
func Pair(ctx context.Context, addr, clientName string, prompt PinPrompter) (deviceID, authKey string, err error) {
	res, err := jointspace.NewSecure(addr).Pair.Run(ctx, jointspace.DeviceInfo{
		DeviceName: clientName,
		DeviceOS:   runtime.GOOS,
		AppID:      "1",
		AppName:    "couchcli",
		Type:       "native",
	}, prompt)
	if err != nil {
		return "", "", err
	}
	return res.DeviceID, res.AuthKey, nil
}

// Settings builds this driver's registry blob from a probe Profile plus the
// pairing credentials (both empty for TVs that need no pairing).
func (p Profile) Settings(deviceID, authKey string) Settings {
	return Settings{
		Port:            p.Port,
		Secure:          p.Secure,
		DeviceID:        deviceID,
		AuthKey:         authKey,
		Name:            p.Name,
		Model:           p.Model,
		SerialNumber:    p.SerialNumber,
		SoftwareVersion: p.SoftwareVersion,
		APIVersion:      p.APIVersion,
	}
}
