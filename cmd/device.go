package cmd

import (
	"fmt"

	"github.com/chrris99/couchcli/internal/config"
	"github.com/chrris99/couchcli/internal/philips"
)

// resolveDeviceEntry returns the saved device entry for alias. If alias is ""
// the default entry is used; if there's no default but exactly one device is
// stored, that one is used.
func resolveDeviceEntry(alias string) (*config.Device, string, error) {
	schema, _, err := config.Load()
	if err != nil {
		return nil, "", err
	}
	if len(schema.Devices) == 0 {
		return nil, "", fmt.Errorf("no paired devices — run `couch pair` first")
	}
	if alias == "" {
		alias = schema.Default
	}
	if alias == "" {
		if len(schema.Devices) == 1 {
			for k := range schema.Devices {
				alias = k
			}
		} else {
			return nil, "", fmt.Errorf("no default device — pass --device <alias> (paired: %s)", deviceAliases(schema))
		}
	}
	d, ok := schema.Get(alias)
	if !ok {
		return nil, "", fmt.Errorf("no device %q (paired: %s)", alias, deviceAliases(schema))
	}
	return d, alias, nil
}

// openClient resolves an alias and builds a JointSpace client preloaded with
// the stored digest credentials.
func openClient(alias string) (*philips.Client, string, error) {
	entry, resolvedAlias, err := resolveDeviceEntry(alias)
	if err != nil {
		return nil, "", err
	}
	var c *philips.Client
	if entry.Secure {
		c = philips.NewSecure(entry.Address)
	} else {
		c = philips.NewPlain(entry.Address)
	}
	if entry.DeviceID != "" && entry.AuthKey != "" {
		c.WithDigest(entry.DeviceID, entry.AuthKey)
	}
	return c, resolvedAlias, nil
}

func deviceAliases(s *config.Schema) string {
	out := ""
	first := true
	for a := range s.Devices {
		if !first {
			out += ", "
		}
		out += a
		first = false
	}
	return out
}
