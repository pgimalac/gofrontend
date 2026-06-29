// Negative test: a struct containing a func field is not comparable.
package main

type NotComparable struct{ f func() }

func Key[K comparable](k K) {}

func main() { Key(NotComparable{}) }
