package testdata

type Y struct {
	F1 int
}

type X struct {
	F1  int
	F2  string
	F3  []byte
	F4  []int
	F5  [3]int
	F6  map[int]int // different keys
	F7  map[int][]int
	F8  []map[int]int
	F9  *int
	F10 *[]int
	F11 []map[int]*[]int
	F12 Y
	F13 []*Y
	F14 map[Y]int
	F15 map[int]*Y
	F16 [][]int
	F17 map[int]map[int]int
}

type A struct {
	d []map[string][]int
}

type B struct {
	a A
	b int
}
