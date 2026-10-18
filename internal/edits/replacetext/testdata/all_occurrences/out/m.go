package m

const max = 10

func limit(s []string, n int) []string { return s[:min(n, len(s))] }

func A(errs []string) []string { return limit(errs, max) }

func B(errs []string) []string { return limit(errs, max) }
