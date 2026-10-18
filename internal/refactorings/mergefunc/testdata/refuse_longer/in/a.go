package m

func f() {}

func a() { f() }

func b() {
	f()
	f()
}
