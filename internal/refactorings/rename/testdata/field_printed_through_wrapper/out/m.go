package m

import "fmt"

type rec struct {
	Title string
}

func logf(format string, args ...any) { fmt.Printf(format, args...) }

type logger struct{ prefix string }

func (l *logger) debugf(format string, args ...any) { logf(format, args...) }

func Show(r rec) { logf("%+v", r) }

func Debug(l *logger, r rec) { l.debugf("%v", r) }
