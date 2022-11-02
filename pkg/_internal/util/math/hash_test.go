package math_test

import (
	"github.com/kuadrant/kcp-glbc/pkg/_internal/util/math"
	"testing"
)

func TestHashString(t *testing.T) {

	someString := "kcp-glbc-24933749"
	someString = math.HashString(someString)

	if someString != "agrS4f0o5gUNZkFTEZSWTCh5P14EfAfkBuzOBI" {
		t.Errorf("Unexpected hashed string value, expected agrS4f0o5gUNZkFTEZSWTCh5P14EfAfkBuzOBI, got %s", someString)
	}
}
