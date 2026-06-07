package installer

import "github.com/homegrew/grew/pkg/release"


import (
	"fmt"
	"github.com/homegrew/grew/pkg/osvdev"
)

// CheckOSVForVersion queries OSV.dev for known vulnerabilities affecting the specified version.
func CheckOSVForVersion(pkgName, targetVer string) (*OSVResult, error) {
	client := osvdev.NewClient()
	q := osvdev.QueryPackage{
		RepoURL: pkgName, // In our usage "github.com/homegrew/grew" acts as the pseudo-repo
		Version: targetVer,
	}
	vulns, err := client.Query(q)
	if err != nil {
		return nil, err
	}
	if len(vulns) > 0 {
		return &OSVResult{
			Vulnerable: true,
			Message:    fmt.Sprintf("found %d known vulnerability(s) in %s", len(vulns), targetVer),
		}, nil
	}
	return &OSVResult{Vulnerable: false}, nil
}
func FileHashes(path string) (string, string, error) {
	return fileHashes(path)
}
