package device

// DriverID identifies which driver controls a device. It is an open enum:
// each driver package exports its own constant (e.g. philips.ID), and
// unknown values must survive config round-trips — a config written by a
// newer couchcli loads fine and fails only when the entry is used, with
// "unknown driver".
type DriverID string
