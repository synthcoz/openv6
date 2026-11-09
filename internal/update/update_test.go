package update

import "testing"

func TestNewer(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{{"v0.2.0", "0.1.9", true}, {"0.1.0", "v0.1.0", false}, {"v1.0.0", "0.9.9", true}, {"0.1.0", "0.1.1", false}, {"v0.10.0", "0.9.0", true}}
	for _, c := range cases {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%s,%s)=%v", c.a, c.b, got)
		}
	}
}
