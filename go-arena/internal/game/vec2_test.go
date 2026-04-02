package game

import (
	"math"
	"testing"
)

func TestVec2Basics(t *testing.T) {
	v := NewVec2(3, 4)
	if v.X() != 3 || v.Y() != 4 {
		t.Fatalf("NewVec2: got (%v,%v), want (3,4)", v.X(), v.Y())
	}

	w := v.WithX(10)
	if w.X() != 10 || w.Y() != 4 {
		t.Errorf("WithX: got (%v,%v)", w.X(), w.Y())
	}
	w = v.WithY(20)
	if w.X() != 3 || w.Y() != 20 {
		t.Errorf("WithY: got (%v,%v)", w.X(), w.Y())
	}
}

func TestVec2Length(t *testing.T) {
	v := NewVec2(3, 4)
	if got := v.Length(); math.Abs(got-5) > 1e-9 {
		t.Errorf("Length: got %v, want 5", got)
	}
	zero := NewVec2(0, 0)
	if zero.Length() != 0 {
		t.Errorf("zero vector length != 0")
	}
}

func TestVec2Add(t *testing.T) {
	a := NewVec2(1, 2)
	b := NewVec2(3, 4)
	c := a.Add(b)
	if c.X() != 4 || c.Y() != 6 {
		t.Errorf("Add: got (%v,%v)", c.X(), c.Y())
	}
}

func TestVec2Sub(t *testing.T) {
	a := NewVec2(5, 7)
	b := NewVec2(2, 3)
	c := a.Sub(b)
	if c.X() != 3 || c.Y() != 4 {
		t.Errorf("Sub: got (%v,%v)", c.X(), c.Y())
	}
}

func TestVec2Scale(t *testing.T) {
	v := NewVec2(2, 3)
	s := v.Scale(2)
	if s.X() != 4 || s.Y() != 6 {
		t.Errorf("Scale: got (%v,%v)", s.X(), s.Y())
	}
}

func TestVec2DistanceTo(t *testing.T) {
	a := NewVec2(0, 0)
	b := NewVec2(3, 4)
	if d := a.DistanceTo(b); math.Abs(d-5) > 1e-9 {
		t.Errorf("DistanceTo: got %v, want 5", d)
	}
}

func TestVec2Normalized(t *testing.T) {
	v := NewVec2(3, 4)
	n := v.Normalized()
	if math.Abs(n.Length()-1) > 1e-9 {
		t.Errorf("Normalized length: %v", n.Length())
	}
	// zero vector normalized -> zero
	z := NewVec2(0, 0).Normalized()
	if z.X() != 0 || z.Y() != 0 {
		t.Errorf("zero normalized not zero: %v", z)
	}
}

func TestVec2JSON(t *testing.T) {
	v := NewVec2(1.5, -2.5)
	data, err := v.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	var v2 Vec2
	if err := v2.UnmarshalJSON(data); err != nil {
		t.Fatal(err)
	}
	if v2.X() != 1.5 || v2.Y() != -2.5 {
		t.Errorf("round-trip: got %v", v2)
	}
}

func TestVec2UnmarshalObjectForm(t *testing.T) {
	data := []byte(`{"x":7.0,"y":8.0}`)
	var v Vec2
	if err := v.UnmarshalJSON(data); err != nil {
		t.Fatal(err)
	}
	if v.X() != 7 || v.Y() != 8 {
		t.Errorf("object form: got %v", v)
	}
}
