package lib

import (
	"fmt"
	"strings"
)

func Describe[T any](v T) string { return fmt.Sprintf("[%v]", v) }

func Join[T any](xs []T) string {
	parts := make([]string, len(xs))
	for i, v := range xs {
		parts[i] = fmt.Sprintf("%v", v)
	}
	return strings.Join(parts, ",")
}
