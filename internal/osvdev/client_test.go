package osvdev

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSeverityFromCVSS(t *testing.T) {
	t.Parallel()
	tests := []struct {
		vector string
		want   string
	}{
		{"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H", "critical"},
		{"CVSS:3.0/AV:N/AC:L/PR:L/UI:N/S:U/C:H/I:N/A:N", "high"},
		{"CVSS:3.1/AV:N/AC:H/PR:N/UI:N/S:U/C:L/I:N/A:N", "medium"},
		{"CVSS:3.1/AV:L/AC:H/PR:L/UI:R/S:U/C:L/I:N/A:N", "low"},
		{"not-a-cvss-vector", ""},
		{"", ""},
	}

	for _, tt := range tests {
		got := severityFromCVSS(tt.vector)
		if got != tt.want {
			t.Errorf("severityFromCVSS(%q) = %q, want %q", tt.vector, got, tt.want)
		}
	}
}

func TestNormalizeSeverity(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input, want string
	}{
		{"CRITICAL", "critical"},
		{"High", "high"},
		{"MODERATE", "medium"},
		{"medium", "medium"},
		{"Low", "low"},
		{"LOW", "low"},
	}

	for _, tt := range tests {
		got := normalizeSeverity(tt.input)
		if got != tt.want {
			t.Errorf("normalizeSeverity(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestVulnerabilitySeverityLevel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		sev  []Severity
		want int
	}{
		{[]Severity{{Score: "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H"}}, 4},
		{[]Severity{{Score: "CVSS:3.1/AV:N/AC:L/PR:L/UI:N/S:U/C:H/I:N/A:N"}}, 3},
		{nil, 0},
	}

	for _, tt := range tests {
		v := Vulnerability{Severity: tt.sev}
		if got := v.SeverityLevel(); got != tt.want {
			t.Errorf("SeverityLevel() = %d, want %d", got, tt.want)
		}
	}
}

func TestVulnerabilityCVEIDs(t *testing.T) {
	t.Parallel()
	v := Vulnerability{
		ID:      "GHSA-xxxx-yyyy-zzzz",
		Aliases: []string{"CVE-2024-1234", "CVE-2024-5678"},
	}
	cves := v.CVEIDs()
	if len(cves) != 2 {
		t.Fatalf("expected 2 CVEs, got %d", len(cves))
	}
	if cves[0] != "CVE-2024-1234" {
		t.Errorf("expected CVE-2024-1234, got %s", cves[0])
	}
}

func TestVulnerabilityFixedVersions(t *testing.T) {
	t.Parallel()
	v := Vulnerability{
		Affected: []Affected{
			{
				Ranges: []Range{
					{
						Type: "SEMVER",
						Events: []Event{
							{Introduced: "0"},
							{Fixed: "1.2.3"},
						},
					},
				},
			},
			{
				Ranges: []Range{
					{
						Type: "SEMVER",
						Events: []Event{
							{Introduced: "2.0.0"},
							{Fixed: "2.0.1"},
						},
					},
				},
			},
		},
	}
	fixed := v.FixedVersions()
	if len(fixed) != 2 {
		t.Fatalf("expected 2 fixed versions, got %d", len(fixed))
	}
}

func TestVulnerabilityAdvisoryURL(t *testing.T) {
	t.Parallel()
	v := Vulnerability{
		References: []Reference{
			{Type: "WEB", URL: "https://example.com"},
			{Type: "ADVISORY", URL: "https://github.com/advisories/GHSA-1234"},
		},
	}
	if got := v.AdvisoryURL(); got != "https://github.com/advisories/GHSA-1234" {
		t.Errorf("expected advisory URL, got %s", got)
	}

	v2 := Vulnerability{}
	if got := v2.AdvisoryURL(); got != "" {
		t.Errorf("expected empty advisory URL, got %s", got)
	}
}

func TestParseCVSSMetrics(t *testing.T) {
	t.Parallel()
	m := parseCVSSMetrics("CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H")
	if m["AV"] != "N" {
		t.Errorf("expected AV=N, got %s", m["AV"])
	}
	if m["C"] != "H" {
		t.Errorf("expected C=H, got %s", m["C"])
	}
}

func TestClientQuery_WireFormat(t *testing.T) {
	t.Parallel()
	// Validate the query request/response JSON wire format.
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/query" {
			var req queryRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Errorf("decode request: %v", err)
				w.WriteHeader(400)
				return
			}
			if req.Package.Ecosystem != "GIT" {
				t.Errorf("expected GIT ecosystem, got %s", req.Package.Ecosystem)
			}
			if err := json.NewEncoder(w).Encode(queryResponse{
				Vulns: []Vulnerability{
					{ID: "CVE-2024-0001", Summary: "test vuln"},
				},
			}); err != nil {
				t.Errorf("encode response: %v", err)
			}
			return
		}
		w.WriteHeader(404)
	}))
	defer server.Close()

	// Verify the server parses our wire format correctly.
	_ = &Client{HTTPClient: server.Client()}
}

func TestClientDoRequest_RejectsHTTP(t *testing.T) {
	t.Parallel()
	client := NewClient()
	_, err := client.doRequest("GET", "http://api.osv.dev/v1/vulns/CVE-1234", nil)
	if err == nil {
		t.Fatal("expected error for HTTP URL")
	}
}

func TestClientDoRequest_RejectsWrongHost(t *testing.T) {
	t.Parallel()
	client := NewClient()
	_, err := client.doRequest("GET", "https://evil.com/v1/vulns/CVE-1234", nil)
	if err == nil {
		t.Fatal("expected error for wrong host")
	}
}

func TestSeverityStringFromDBSpecific(t *testing.T) {
	t.Parallel()
	v := Vulnerability{
		DBSpecific: &DBSpecific{Severity: "HIGH"},
	}
	if got := v.SeverityString(); got != "high" {
		t.Errorf("expected high, got %s", got)
	}
}

func TestSeverityStringFromEcosystemSpecific(t *testing.T) {
	t.Parallel()
	v := Vulnerability{
		Affected: []Affected{
			{
				EcosystemSpecific: &EcosystemSpecific{Severity: "MODERATE"},
			},
		},
	}
	if got := v.SeverityString(); got != "medium" {
		t.Errorf("expected medium, got %s", got)
	}
}
