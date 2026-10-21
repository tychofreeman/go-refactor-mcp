package m

import "log"

func logEmail(to string) { logTo(to, "email to %s") }

func logTo(to string, format string) { log.Printf(format, to) }

func logSMS(addr string) { logTo(addr, "sms to %s") }

func use() {
	logEmail("a@x")
	logSMS("555")
}
