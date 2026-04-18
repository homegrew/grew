package osvdev

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

var apiBase = "https://api.osv.dev/v1"

// SetAPIBase overrides the OSV API base URL for testing.
func SetAPIBase(url string) {
	apiBase = url
}

const (
	batchSize   = 1000
	openTimeout = 10 * time.Second
	readTimeout = 30 * time.Second
	maxRetries  = 3
	retryDelay  = 1 * time.Second
)

// Client queries the OSV.dev vulnerability database.
type Client struct {
	HTTPClient *http.Client
}

// NewClient creates an OSV API client with sensible defaults.
func NewClient() *Client {
	return &Client{
		HTTPClient: &http.Client{
			Timeout: openTimeout + readTimeout,
		},
	}
}

// QueryPackage holds the input for a single OSV query.
type QueryPackage struct {
	RepoURL string
	Version string
}

// Vulnerability represents a single OSV vulnerability record.
type Vulnerability struct {
	ID         string      `json:"id"`
	Summary    string      `json:"summary"`
	Details    string      `json:"details"`
	Aliases    []string    `json:"aliases"`
	Severity   []Severity  `json:"severity"`
	References []Reference `json:"references"`
	Affected   []Affected  `json:"affected"`
	DBSpecific *DBSpecific `json:"database_specific,omitempty"`
}

// Severity holds a CVSS score vector.
type Severity struct {
	Type  string `json:"type"`
	Score string `json:"score"`
}

// Reference is a URL reference associated with a vulnerability.
type Reference struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

// Affected describes affected packages and version ranges.
type Affected struct {
	Package           *Package                   `json:"package,omitempty"`
	Ranges            []Range                    `json:"ranges"`
	Versions          []string                   `json:"versions"`
	EcosystemSpecific *EcosystemSpecific         `json:"ecosystem_specific,omitempty"`
	DatabaseSpecific  map[string]json.RawMessage `json:"database_specific,omitempty"`
}

// Package identifies the affected package.
type Package struct {
	Name      string `json:"name"`
	Ecosystem string `json:"ecosystem"`
	Purl      string `json:"purl,omitempty"`
}

// Range describes version ranges affected by a vulnerability.
type Range struct {
	Type   string  `json:"type"`
	Events []Event `json:"events"`
}

// Event is a version event (introduced, fixed, last_affected).
type Event struct {
	Introduced   string `json:"introduced,omitempty"`
	Fixed        string `json:"fixed,omitempty"`
	LastAffected string `json:"last_affected,omitempty"`
}

// EcosystemSpecific holds ecosystem-specific metadata.
type EcosystemSpecific struct {
	Severity string `json:"severity,omitempty"`
}

// DBSpecific holds database-specific metadata.
type DBSpecific struct {
	Severity string `json:"severity,omitempty"`
}

// queryRequest is the JSON body for /query.
type queryRequest struct {
	Package   queryPackage `json:"package"`
	Version   string       `json:"version"`
	PageToken string       `json:"page_token,omitempty"`
}

type queryPackage struct {
	Name      string `json:"name"`
	Ecosystem string `json:"ecosystem"`
}

// queryResponse is the JSON response from /query.
type queryResponse struct {
	Vulns         []Vulnerability `json:"vulns"`
	NextPageToken string          `json:"next_page_token"`
}

// batchRequest is the JSON body for /querybatch.
type batchRequest struct {
	Queries []queryRequest `json:"queries"`
}

// batchResponse is the JSON response from /querybatch.
type batchResponse struct {
	Results []batchResultEntry `json:"results"`
}

type batchResultEntry struct {
	Vulns []Vulnerability `json:"vulns"`
}

// Query checks a single package against the OSV database.
func (c *Client) Query(pkg QueryPackage) ([]Vulnerability, error) {
	req := queryRequest{
		Package: queryPackage{
			Name:      pkg.RepoURL,
			Ecosystem: "GIT",
		},
		Version: pkg.Version,
	}

	resp, err := c.post("/query", req)
	if err != nil {
		return nil, err
	}

	var qr queryResponse
	if err := json.Unmarshal(resp, &qr); err != nil {
		return nil, fmt.Errorf("parse OSV response: %w", err)
	}

	vulns := qr.Vulns

	// Paginate.
	for qr.NextPageToken != "" {
		req.PageToken = qr.NextPageToken
		resp, err = c.post("/query", req)
		if err != nil {
			return nil, err
		}
		qr = queryResponse{}
		if err := json.Unmarshal(resp, &qr); err != nil {
			return nil, fmt.Errorf("parse OSV response: %w", err)
		}
		vulns = append(vulns, qr.Vulns...)
	}

	return vulns, nil
}

// QueryBatch checks multiple packages in a single request.
// Returns a slice of vulnerability lists, one per input package.
func (c *Client) QueryBatch(packages []QueryPackage) ([][]Vulnerability, error) {
	results := make([][]Vulnerability, len(packages))

	for start := 0; start < len(packages); start += batchSize {
		end := start + batchSize
		if end > len(packages) {
			end = len(packages)
		}
		batch := packages[start:end]

		queries := make([]queryRequest, len(batch))
		for i, pkg := range batch {
			queries[i] = queryRequest{
				Package: queryPackage{
					Name:      pkg.RepoURL,
					Ecosystem: "GIT",
				},
				Version: pkg.Version,
			}
		}

		resp, err := c.post("/querybatch", batchRequest{Queries: queries})
		if err != nil {
			return nil, err
		}

		var br batchResponse
		if err := json.Unmarshal(resp, &br); err != nil {
			return nil, fmt.Errorf("parse OSV batch response: %w", err)
		}

		for i, entry := range br.Results {
			results[start+i] = entry.Vulns
		}
	}

	return results, nil
}

// GetVulnerability fetches full details for a vulnerability by ID.
func (c *Client) GetVulnerability(id string) (*Vulnerability, error) {
	escaped := url.PathEscape(id)
	resp, err := c.get("/vulns/" + escaped)
	if err != nil {
		return nil, err
	}

	var v Vulnerability
	if err := json.Unmarshal(resp, &v); err != nil {
		return nil, fmt.Errorf("parse OSV vulnerability: %w", err)
	}
	return &v, nil
}

// post sends a POST request with JSON body.
func (c *Client) post(path string, body any) ([]byte, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	return c.doRequest("POST", apiBase+path, data)
}

// get sends a GET request.
func (c *Client) get(path string) ([]byte, error) {
	return c.doRequest("GET", apiBase+path, nil)
}

// doRequest executes an HTTP request with retry logic.
func (c *Client) doRequest(method, rawURL string, body []byte) ([]byte, error) {
	// Validate URL to prevent SSRF.
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}

	expected, err := url.Parse(apiBase)
	if err != nil {
		return nil, fmt.Errorf("invalid API base URL: %w", err)
	}

	if u.Scheme != expected.Scheme {
		return nil, fmt.Errorf("OSV API requires %s, got %s", expected.Scheme, u.Scheme)
	}
	if u.Host != expected.Host {
		return nil, fmt.Errorf("unexpected OSV API host: %s (expected %s)", u.Host, expected.Host)
	}

	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(retryDelay)
		}

		var reqBody io.Reader
		if body != nil {
			reqBody = bytes.NewReader(body)
		}

		req := &http.Request{
			Method:     method,
			URL:        u,
			Proto:      "HTTP/1.1",
			ProtoMajor: 1,
			ProtoMinor: 1,
			Header:     make(http.Header),
			Host:       u.Host,
			Body:       io.NopCloser(reqBody),
		}
		if reqBody != nil {
			// bytes.Reader len cast
			if br, ok := reqBody.(*bytes.Reader); ok {
				req.ContentLength = int64(br.Len())
			}
		}

		req.Header.Set("Content-Type", "application/json")

		resp, err := c.HTTPClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("OSV API request failed: %w", err)
			continue
		}

		respBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("read response: %w", err)
			continue
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return respBody, nil
		}

		lastErr = fmt.Errorf("OSV API error: %d %s", resp.StatusCode, resp.Status)
		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			// Client error — don't retry.
			return nil, lastErr
		}
	}

	return nil, fmt.Errorf("OSV API failed after %d attempts: %w", maxRetries, lastErr)
}

// SeverityLevel returns a numeric severity level from a vulnerability.
// 4=critical, 3=high, 2=medium, 1=low, 0=unknown.
func (v *Vulnerability) SeverityLevel() int {
	sev := v.SeverityString()
	switch sev {
	case "critical":
		return 4
	case "high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	default:
		return 0
	}
}

// SeverityString extracts the human-readable severity from the vulnerability.
func (v *Vulnerability) SeverityString() string {
	// Try CVSS score first.
	for _, s := range v.Severity {
		if sev := severityFromCVSS(s.Score); sev != "" {
			return sev
		}
	}

	// Try database_specific.severity.
	if v.DBSpecific != nil && v.DBSpecific.Severity != "" {
		return normalizeSeverity(v.DBSpecific.Severity)
	}

	// Try ecosystem_specific.severity in affected entries.
	for _, aff := range v.Affected {
		if aff.EcosystemSpecific != nil && aff.EcosystemSpecific.Severity != "" {
			return normalizeSeverity(aff.EcosystemSpecific.Severity)
		}
	}

	return "unknown"
}

// CVEIDs returns all CVE identifiers associated with this vulnerability.
func (v *Vulnerability) CVEIDs() []string {
	var cves []string
	if len(v.ID) > 4 && v.ID[:4] == "CVE-" {
		cves = append(cves, v.ID)
	}
	for _, alias := range v.Aliases {
		if len(alias) > 4 && alias[:4] == "CVE-" {
			cves = append(cves, alias)
		}
	}
	return cves
}

// AdvisoryURL returns the first ADVISORY reference URL, if any.
func (v *Vulnerability) AdvisoryURL() string {
	for _, ref := range v.References {
		if ref.Type == "ADVISORY" {
			return ref.URL
		}
	}
	return ""
}

// FixedVersions returns all "fixed" version strings from affected ranges.
func (v *Vulnerability) FixedVersions() []string {
	seen := make(map[string]bool)
	var versions []string
	for _, aff := range v.Affected {
		for _, r := range aff.Ranges {
			for _, ev := range r.Events {
				if ev.Fixed != "" && !seen[ev.Fixed] {
					seen[ev.Fixed] = true
					versions = append(versions, ev.Fixed)
				}
			}
		}
	}
	return versions
}

// severityFromCVSS extracts severity from a CVSS:3.x vector string.
func severityFromCVSS(vector string) string {
	if len(vector) < 8 {
		return ""
	}
	// Must contain CVSS:3
	found := false
	for i := 0; i <= len(vector)-6; i++ {
		if vector[i:i+6] == "CVSS:3" {
			found = true
			break
		}
	}
	if !found {
		return ""
	}

	metrics := parseCVSSMetrics(vector)
	if len(metrics) == 0 {
		return ""
	}

	impactHigh := 0
	for _, m := range []string{"C", "I", "A"} {
		if metrics[m] == "H" {
			impactHigh++
		}
	}
	networkAttack := metrics["AV"] == "N"
	noPrivs := metrics["PR"] == "N"
	noInteraction := metrics["UI"] == "N"

	switch {
	case impactHigh >= 2 && networkAttack && noPrivs:
		return "critical"
	case impactHigh >= 1 && networkAttack:
		return "high"
	case impactHigh >= 1 || (networkAttack && noPrivs && noInteraction):
		return "medium"
	default:
		return "low"
	}
}

// parseCVSSMetrics parses "AV:N/AC:L/..." into a map.
func parseCVSSMetrics(vector string) map[string]string {
	metrics := make(map[string]string)
	i := 0
	for i < len(vector) {
		// Find next uppercase letter sequence followed by ':'
		keyStart := -1
		for i < len(vector) {
			if vector[i] >= 'A' && vector[i] <= 'Z' {
				keyStart = i
				break
			}
			i++
		}
		if keyStart < 0 {
			break
		}

		// Read key.
		j := keyStart
		for j < len(vector) && vector[j] >= 'A' && vector[j] <= 'Z' {
			j++
		}
		if j >= len(vector) || vector[j] != ':' {
			i = j + 1
			continue
		}
		key := vector[keyStart:j]
		j++ // skip ':'

		// Read value (single uppercase letter).
		if j < len(vector) && vector[j] >= 'A' && vector[j] <= 'Z' {
			metrics[key] = string(vector[j])
			i = j + 1
		} else {
			i = j
		}
	}
	return metrics
}

// normalizeSeverity normalizes severity strings to lowercase canonical form.
func normalizeSeverity(s string) string {
	lower := ""
	for _, c := range s {
		if c >= 'A' && c <= 'Z' {
			lower += string(c + 32)
		} else {
			lower += string(c)
		}
	}
	switch lower {
	case "critical":
		return "critical"
	case "high":
		return "high"
	case "moderate", "medium":
		return "medium"
	case "low":
		return "low"
	default:
		return lower
	}
}
