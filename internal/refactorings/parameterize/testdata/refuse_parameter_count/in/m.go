package m

import "log"

func logEmail(to string) { log.Printf("email to %s", to) }

func logSMS(addr string) { log.Printf("sms to %s", addr) }
