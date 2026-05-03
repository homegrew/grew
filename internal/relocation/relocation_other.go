//go:build !darwin

package relocation

func inspectBinary(_ string) ([]string, error) {
	return nil, nil
}

func relocateBinary(_ string, _ Replacements) error {
	return nil
}

func verifyBinary(_, _ string) []Issue {
	return nil
}
