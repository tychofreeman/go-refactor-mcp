import { logged } from "./log.ts";
import { broadcast, Email, SMS, type User } from "./notify.ts";
import { equal } from "./testing.ts";

const e = new Email();
const s = new SMS();
const u: User = { id: "u1", name: "Ann", contacts: { email: "a@x", sms: "555" } };
const got = broadcast({ channels: [e, s] }, [u], { subject: "A long subject for a text", body: "" });
equal(got, ["Ann: email,sms"], "broadcast");
equal(e.sent, ["a@x: A long subject for a text"], "email sent");
equal(s.sent, ["555: A long subject for a"], "sms sent");
equal(logged, ["email to a@x", "sms to 555"], "logged");
