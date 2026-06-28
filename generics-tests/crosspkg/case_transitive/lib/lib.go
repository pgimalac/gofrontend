package lib

import "xtest/case_transitive/base"

func WrapOnce[T any](v T) base.Box[T] { return base.Wrap(v) }
func Unwrap[T any](b base.Box[T]) T   { return b.Get() }
