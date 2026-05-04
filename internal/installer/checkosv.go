package installer

import "github.com/homegrew/grew/internal/release"

import (
	"fmt"
	"github.com/homegrew/grew/internal/osvdev"
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
	sha256Hash, err := release.FileSHA256(path)
	if err != nil {
		return "", "", err
	}
	sha512Hash, err := release.FileSHA512(path)
	if err != nil {
		return "", "", err
	}
	return sha256Hash, sha512Hash, nil
}
