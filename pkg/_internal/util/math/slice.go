package math

import (
	"golang.org/x/exp/constraints"
)

func Max[T constraints.Ordered](values ...T) T {
	var currentMax T

	for _, value := range values {
		if value >= currentMax {
			currentMax = value
		}
	}

	return currentMax
}

func Min[T constraints.Ordered](values ...T) T {
	var currentMin T

	for _, value := range values {
		if value < currentMin {
			currentMin = value
		}
	}

	return currentMin
}
