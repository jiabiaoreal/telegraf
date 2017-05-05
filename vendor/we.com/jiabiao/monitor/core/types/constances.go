package types

import "regexp"

var (
	hostrex = regexp.MustCompile(`^[a-z0-9A-Z][-a-z0-9A-Z.]*`)
	iprex   = regexp.MustCompile(`^([0-9]{1,3}.){3}[0-9]{1,3}`)
)
