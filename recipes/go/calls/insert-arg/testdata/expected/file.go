package p

import "example.com/lib"

func F(ctx any) {
	lib.Connect(ctx, "a")
}
