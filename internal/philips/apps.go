package philips

import (
	"context"
	"net/http"
)

// AppsService handles JointSpace /6/applications and /6/activities endpoints.
type AppsService service

// Component is the Android (packageName, className) pair that identifies an
// activity on the TV.
type Component struct {
	PackageName string `json:"packageName"`
	ClassName   string `json:"className"`
}

// Intent is the launch payload accepted by /6/activities/launch. Callers
// receive whatever the TV reports verbatim and POST it back unchanged.
type Intent struct {
	Component Component      `json:"component"`
	Action    string         `json:"action,omitempty"`
	Extras    map[string]any `json:"extras,omitempty"`
}

// Application is one entry in GET /6/applications.
type Application struct {
	Label  string `json:"label"`
	Order  int    `json:"order,omitempty"`
	ID     string `json:"id,omitempty"`
	Type   string `json:"type,omitempty"`
	Intent Intent `json:"intent"`
}

type applicationsResponse struct {
	Applications []Application `json:"applications"`
	Version      int           `json:"version,omitempty"`
}

// List returns the installed apps the TV exposes via JointSpace.
func (s *AppsService) List(ctx context.Context) ([]Application, error) {
	var resp applicationsResponse
	if err := s.client.Do(ctx, http.MethodGet, "/6/applications", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Applications, nil
}

// Current returns the foreground component. When no activity is in front
// (e.g. on the system launcher) PackageName may be empty.
func (s *AppsService) Current(ctx context.Context) (Component, error) {
	var resp struct {
		Component Component `json:"component"`
	}
	if err := s.client.Do(ctx, http.MethodGet, "/6/activities/current", nil, &resp); err != nil {
		return Component{}, err
	}
	return resp.Component, nil
}

// Launch POSTs an intent to /6/activities/launch.
func (s *AppsService) Launch(ctx context.Context, intent Intent) error {
	return s.client.Do(ctx, http.MethodPost, "/6/activities/launch", intent, nil)
}

// Home sends the Home key, returning to the system launcher.
func (s *AppsService) Home(ctx context.Context) error {
	return s.client.Keys.Send(ctx, KeyHome)
}
