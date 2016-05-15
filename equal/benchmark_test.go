package equal

import (
	"reflect"
	"testing"

	"github.com/gadumitrachioaiei/goequal/equal/testdata"
)

var r1 bool

func BenchmarkGoEqualX(b *testing.B) {
	var t1, t2 testdata.A
	var r bool
	for n := 0; n < b.N; n++ {
		r = testdata.EqualA(&t1, &t2)
	}
	r1 = r
}

func BenchmarkReflectX(b *testing.B) {
	var t1, t2 testdata.A
	var r bool
	for n := 0; n < b.N; n++ {
		r = reflect.DeepEqual(&t1, &t2)
	}
	r1 = r
}

func BenchmarkGoEqualY(b *testing.B) {
	var t1, t2 testdata.B
	var r bool
	for n := 0; n < b.N; n++ {
		r = testdata.EqualB(&t1, &t2)
	}
	r1 = r
}

func BenchmarkReflectY(b *testing.B) {
	var t1, t2 testdata.B
	var r bool
	for n := 0; n < b.N; n++ {
		r = reflect.DeepEqual(&t1, &t2)
	}
	r1 = r
}
