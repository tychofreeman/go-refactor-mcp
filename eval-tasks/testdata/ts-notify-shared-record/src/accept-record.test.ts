import { logged } from "./log.ts";
import { broadcast, Email, record, SMS } from "./notify.ts";
import { equal } from "./testing.ts";

const sent: string[] = [];
record(sent, "pigeon", "roof", "hello");
equal(sent, ["roof: hello"], "record pushed");
equal(logged, ["pigeon to roof"], "record logged");
logged.length = 0;
const e = new Email();
const s = new SMS();
broadcast({ channels: [e, s] }, [{ id: "u1", name: "Ann", email: "a@x", phone: "555" }], { subject: "A long subject for a text", body: "" });
equal(logged, ["email to a@x", "sms to 555"], "sending logged");
equal(e.sent, ["a@x: A long subject for a text"], "email sent");
equal(s.sent, ["555: A long subject for a"], "sms sent");
