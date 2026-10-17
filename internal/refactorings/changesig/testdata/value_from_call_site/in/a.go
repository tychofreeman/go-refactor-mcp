package m

type Config struct{ Dir, OutDir string }

func copyTree(src, dst string) error { return nil }

func run(cfg Config) error {
	return copyTree(cfg.Dir, "/tmp/x")
}
