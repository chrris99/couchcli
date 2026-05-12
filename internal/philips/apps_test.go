package philips

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListApplications(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/6/applications" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"applications": []map[string]any{
				{
					"label": "Netflix",
					"intent": map[string]any{
						"component": map[string]any{
							"packageName": "com.netflix.ninja",
							"className":   "com.netflix.ninja.MainActivity",
						},
						"action": "android.intent.action.MAIN",
					},
				},
				{
					"label": "YouTube",
					"intent": map[string]any{
						"component": map[string]any{
							"packageName": "com.google.android.youtube.tv",
							"className":   "com.google.android.apps.youtube.tv.activity.ShellActivity",
						},
					},
				},
			},
		})
	}))
	defer srv.Close()

	apps, err := newTestClient(t, srv).Apps.List(context.Background())
	if err != nil {
		t.Fatalf("Apps.List: %v", err)
	}
	if len(apps) != 2 {
		t.Fatalf("want 2 apps, got %d", len(apps))
	}
	if apps[0].Label != "Netflix" || apps[0].Intent.Component.PackageName != "com.netflix.ninja" {
		t.Errorf("apps[0]=%+v", apps[0])
	}
	if apps[1].Intent.Component.PackageName != "com.google.android.youtube.tv" {
		t.Errorf("apps[1] package=%q", apps[1].Intent.Component.PackageName)
	}
}

func TestGetCurrentActivity(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/6/activities/current" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"component": map[string]any{
				"packageName": "com.netflix.ninja",
				"className":   "com.netflix.ninja.MainActivity",
			},
		})
	}))
	defer srv.Close()

	comp, err := newTestClient(t, srv).Apps.Current(context.Background())
	if err != nil {
		t.Fatalf("Apps.Current: %v", err)
	}
	if comp.PackageName != "com.netflix.ninja" {
		t.Errorf("PackageName=%q", comp.PackageName)
	}
}

func TestLaunchApplication_BodyShape(t *testing.T) {
	var captured map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/6/activities/launch" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &captured)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	intent := Intent{
		Component: Component{
			PackageName: "com.netflix.ninja",
			ClassName:   "com.netflix.ninja.MainActivity",
		},
		Action: "android.intent.action.MAIN",
	}
	if err := newTestClient(t, srv).Apps.Launch(context.Background(), intent); err != nil {
		t.Fatalf("Apps.Launch: %v", err)
	}
	comp, ok := captured["component"].(map[string]any)
	if !ok {
		t.Fatalf("body missing component: %+v", captured)
	}
	if comp["packageName"] != "com.netflix.ninja" {
		t.Errorf("packageName=%v", comp["packageName"])
	}
	if comp["className"] != "com.netflix.ninja.MainActivity" {
		t.Errorf("className=%v", comp["className"])
	}
	if captured["action"] != "android.intent.action.MAIN" {
		t.Errorf("action=%v", captured["action"])
	}
}
