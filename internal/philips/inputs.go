package philips

// InputsService consolidates the two paths Philips firmware uses for HDMI
// source switching: classic /6/sources (older firmware, some Android models)
// and HDMI-as-app via /6/applications + /6/activities/launch (Android 2017+).
// Callers should not have to care which one their TV supports.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

// InputsService handles JointSpace /6/sources + the HDMI-as-app fallback.
type InputsService service

// Input is one selectable signal source on the TV.
type Input struct {
	ID     string
	Label  string
	Kind   string // "hdmi", "tv", "app", "" if unknown
	Intent Intent // populated when the TV needs an intent-launch to switch (Android HDMI-as-app)
}

// source is one entry returned by GET /6/sources.
type source struct {
	ID   string
	Name string
}

// sourcesMap is the raw shape of GET /6/sources: object keyed by source ID.
type sourcesMap map[string]struct {
	Name string `json:"name"`
}

// IsNotFound reports whether err is an HTTPError with status 404. Used to
// branch on firmware variants that drop endpoints.
func IsNotFound(err error) bool {
	var he *HTTPError
	return errors.As(err, &he) && he.Status == 404
}

// List returns the inputs the TV exposes. Tries /6/sources first; if that's
// 404 or empty, falls back to filtering /6/applications for HDMI entries.
// A nil error with an empty slice means the TV has no enumerable inputs but
// the on-screen picker may still work (see OpenPicker).
func (s *InputsService) List(ctx context.Context) ([]Input, error) {
	sources, err := s.listSources(ctx)
	switch {
	case err != nil && !IsNotFound(err):
		return nil, err
	case err == nil && len(sources) > 0:
		out := make([]Input, 0, len(sources))
		for _, src := range sources {
			out = append(out, Input{ID: src.ID, Label: src.Name, Kind: inferInputKind(src.ID)})
		}
		return out, nil
	}
	// Fallback for Android firmware.
	apps, err := s.client.Apps.List(ctx)
	if err != nil {
		if IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return hdmiInputsFromApps(apps), nil
}

// Current reports the active input. Resolves the label against the source
// list best-effort; a missing /6/sources doesn't block the result.
func (s *InputsService) Current(ctx context.Context) (Input, error) {
	cur, err := s.getCurrentSource(ctx)
	if err != nil {
		if IsNotFound(err) {
			return Input{}, nil
		}
		return Input{}, err
	}
	in := Input{ID: cur.ID, Kind: inferInputKind(cur.ID)}
	if sources, err := s.listSources(ctx); err == nil {
		for _, src := range sources {
			if src.ID == cur.ID {
				in.Label = src.Name
				break
			}
		}
	}
	return in, nil
}

// Switch activates the given input. If the input carries an intent payload
// (Android HDMI-as-app), launches it via /6/activities/launch; otherwise
// POSTs to /6/sources/current.
func (s *InputsService) Switch(ctx context.Context, in Input) error {
	if in.Intent.Component.PackageName != "" {
		return s.client.Apps.Launch(ctx, in.Intent)
	}
	return s.switchSource(ctx, in.ID)
}

// OpenPicker pops up the on-screen Source selector. Universal fallback when
// neither /6/sources nor the HDMI-as-app trick works.
func (s *InputsService) OpenPicker(ctx context.Context) error {
	return s.client.Keys.Send(ctx, KeySource)
}

// listSources hits GET /6/sources. Many Android-firmware TVs (2017+) return
// an empty object — that's not an error, just means the caller should fall
// back to applications-based HDMI detection.
func (s *InputsService) listSources(ctx context.Context) ([]source, error) {
	var raw sourcesMap
	if err := s.client.Do(ctx, http.MethodGet, "/6/sources", nil, &raw); err != nil {
		return nil, err
	}
	out := make([]source, 0, len(raw))
	for id, v := range raw {
		out = append(out, source{ID: id, Name: v.Name})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// getCurrentSource hits GET /6/sources/current. TV reports only the ID; the
// caller resolves the human name via listSources.
func (s *InputsService) getCurrentSource(ctx context.Context) (source, error) {
	var resp struct {
		ID string `json:"id"`
	}
	if err := s.client.Do(ctx, http.MethodGet, "/6/sources/current", nil, &resp); err != nil {
		return source{}, err
	}
	return source{ID: resp.ID}, nil
}

// switchSource hits POST /6/sources/current with id (e.g. "hdmi1").
func (s *InputsService) switchSource(ctx context.Context, id string) error {
	body := struct {
		ID string `json:"id"`
	}{ID: id}
	return s.client.Do(ctx, http.MethodPost, "/6/sources/current", body, nil)
}

// hdmiInputsFromApps filters /6/applications for HDMI-source entries. Three
// signals identify an HDMI entry (any one suffices):
//
//  1. extras["hdmiPortNumber"] is set (Philips droidtv.playtv convention)
//  2. intent.action contains "HDMI"
//  3. label starts with "HDMI"
//
// Resulting ID is stable: "hdmi<N>" when port number is known, otherwise
// a slug of the label.
func hdmiInputsFromApps(apps []Application) []Input {
	var out []Input
	for _, a := range apps {
		port, ok := detectHDMIPort(a)
		if !ok {
			continue
		}
		id := ""
		if port > 0 {
			id = fmt.Sprintf("hdmi%d", port)
		} else {
			id = labelSlug(a.Label)
		}
		out = append(out, Input{
			ID:     id,
			Label:  a.Label,
			Kind:   "hdmi",
			Intent: a.Intent,
		})
	}
	return out
}

// detectHDMIPort returns (port, true) when the application looks like an HDMI
// source. port is 0 when the entry is HDMI-shaped but the port number isn't
// available.
func detectHDMIPort(a Application) (int, bool) {
	if a.Intent.Extras != nil {
		if v, ok := a.Intent.Extras["hdmiPortNumber"]; ok {
			switch n := v.(type) {
			case float64:
				return int(n), true
			case int:
				return n, true
			case string:
				var port int
				if _, err := fmt.Sscanf(n, "%d", &port); err == nil {
					return port, true
				}
				return 0, true
			}
		}
	}
	upperAction := strings.ToUpper(a.Intent.Action)
	upperLabel := strings.ToUpper(a.Label)
	if strings.Contains(upperAction, "HDMI") || strings.HasPrefix(upperLabel, "HDMI") {
		return portFromLabel(a.Label), true
	}
	return 0, false
}

// portFromLabel parses the first digit run in a label like "HDMI 2" → 2.
// Returns 0 if no digits found.
func portFromLabel(label string) int {
	var n int
	var found bool
	for _, r := range label {
		if r >= '0' && r <= '9' {
			n = n*10 + int(r-'0')
			found = true
		} else if found {
			break
		}
	}
	if !found {
		return 0
	}
	return n
}

func labelSlug(label string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(label) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ':
			b.WriteByte('-')
		}
	}
	if b.Len() == 0 {
		return "input"
	}
	return b.String()
}

func inferInputKind(id string) string {
	switch {
	case len(id) >= 4 && id[:4] == "hdmi":
		return "hdmi"
	case id == "tv":
		return "tv"
	default:
		return ""
	}
}
