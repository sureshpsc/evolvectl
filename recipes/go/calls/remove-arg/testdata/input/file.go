package p

import "example.com/lib"

func F(ctx any) {
	lib.Dial(ctx, "a")
}
