package main
import ("fmt"; "sort")
type Set[T comparable] map[T]bool
func (s Set[T]) Add(x T) { s[x] = true }
func main() {
	s := Set[string]{}
	s.Add("b"); s.Add("a"); s.Add("a")
	keys := []string{}
	for k := range s { keys = append(keys, k) }
	sort.Strings(keys)
	fmt.Println(keys)
}
