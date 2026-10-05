package engine

import (
	"reflect"
	"testing"
)

func TestIntersectAndUnion(t *testing.T) {
	a, b := []uint32{1, 3, 5, 7}, []uint32{3, 4, 7, 9}
	if got := Intersect(a, b); !reflect.DeepEqual(got, []uint32{3, 7}) {
		t.Errorf("intersect = %v, want [3 7]", got)
	}
	if got := Intersect([]uint32{1}, []uint32{2}); got == nil || len(got) != 0 {
		t.Errorf("intersect of disjoint lists = %#v, want an empty, non-nil list (nil means any id)", got)
	}
	if got := Union(a, b); !reflect.DeepEqual(got, []uint32{1, 3, 4, 5, 7, 9}) {
		t.Errorf("union = %v, want [1 3 4 5 7 9]", got)
	}
}
