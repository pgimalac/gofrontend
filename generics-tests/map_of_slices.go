package main
import ("fmt"; "sort")
func GroupByLen[T any](xs []T, key func(T) int) map[int][]T {
	m := map[int][]T{}
	for _, x := range xs { k := key(x); m[k] = append(m[k], x) }
	return m
}
func main() {
	m := GroupByLen([]string{"a","bb","cc","d"}, func(s string) int { return len(s) })
	ks := []int{}; for k := range m { ks = append(ks, k) }; sort.Ints(ks)
	for _, k := range ks { fmt.Println(k, m[k]) }
}
