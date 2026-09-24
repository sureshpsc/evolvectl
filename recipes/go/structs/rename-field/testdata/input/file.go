package p

func F() string {
	cfg := Config{OldField: "x"}
	return cfg.OldField
}
