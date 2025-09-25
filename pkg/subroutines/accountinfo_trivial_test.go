package subroutines

import "testing"

func TestAccountInfoSubroutine_Trivial(t *testing.T) {
	r := NewAccountInfoSubroutine(nil, "CA")
	if r.GetName() == "" {
		t.Fatal("expected name")
	}
	if len(r.Finalizers()) == 0 {
		t.Fatal("expected finalizers")
	}
}
