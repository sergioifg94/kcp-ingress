package math

import "testing"

func TestMax(t *testing.T) {
	values := []int{-40, 0, 9000, 42}
	max := Max(values...)

	if max != 9000 {
		t.Errorf("Unexpected max, expected 9000, got %d", max)
	}

	max = Max[int]()
	if max != 0 {
		t.Errorf("Unexpected max for 0 length, expected 0, got %d", max)
	}
}

func TestMin(t *testing.T) {
	values := []int{-40, -5, 0, 9000, 42}
	min := Min(values...)

	if min != -40 {
		t.Errorf("Unexpected min, expected -40, got %d", min)
	}

	min = Min[int]()
	if min != 0 {
		t.Errorf("Unexpected min for 0 length, expected 0, got %d", min)
	}
}
