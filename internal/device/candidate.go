package device

// Candidate is a device found on the network that a driver claims it can
// control. One Candidate per physical device, protocol-level detail goes in
// Sources and Hints, not extra rows.
type Candidate struct {
	Driver  DriverID `json:"driver"`
	Name    string   `json:"name"`
	Address string   `json:"address"`
	// Sources lists the discovery mechanisms that surfaced this device,
	// e.g. "mdns:_philipstv_s_rpc._tcp", "ssdp:MediaRenderer:1".
	Sources []string `json:"sources,omitempty"`
	// Hints carries driver-owned metadata for display and pairing, e.g.
	// "model", "api", "secure". Keys are driver-defined.
	Hints map[string]string `json:"hints,omitempty"`
}
