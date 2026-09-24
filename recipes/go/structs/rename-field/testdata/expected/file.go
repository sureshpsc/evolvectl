package p

func F() string {
	cfg := Config{NewField: "x"}
	return cfg.NewField
}
