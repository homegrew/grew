package bpatch

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/homegrew/grew/internal/release"
)

func TestUpgradeString(t *testing.T) {
	rel := &release.Release{TagName: "v1.2.0"}
	u := Upgrade{toVersion: "v1.3.0", release: rel}
	expected := "Upgrade from v1.2.0 to v1.3.0"
	if got := u.String(); got != expected {
		t.Errorf("expected %q, got %q", expected, got)
	}
}

func TestIsOfficialBuild(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "v1.0.0") {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"tag_name":"v1.0.0"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	if err := release.SetAPIBase(ts.URL); err != nil {
		t.Fatalf("failed to set api base: %v", err)
	}
	defer release.SetAPIBase("https://api.github.com")

	if !isOfficialBuild("v1.0.0") {
		t.Errorf("expected v1.0.0 to be official build")
	}
	if isOfficialBuild("v999.0.0") {
		t.Errorf("expected v999.0.0 to not be official build")
	}
}
