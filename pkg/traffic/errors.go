package traffic

import (
	"fmt"
)

var (
	ErrInvalidAccessObject  = fmt.Errorf("not a valid traffic object type (expected: ingress or route)")
	ErrGeneratedHostMissing = fmt.Errorf("generated host annotation was expected but was not set. Ensure DNSRecord created")
)

func IsInvalidAccessObjectError(err error) bool {
	return err.Error() == ErrInvalidAccessObject.Error()
}

func IsMissingGeneratedHostError(err error) bool {
	return err.Error() == ErrGeneratedHostMissing.Error()
}
