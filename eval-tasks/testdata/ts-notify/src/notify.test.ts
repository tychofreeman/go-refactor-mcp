import { broadcast, Email, SMS } from "./notify.ts";
import { equal } from "./testing.ts";

const e = new Email();
const s = new SMS();
const users = [{ id: "u1", name: "Ann", email: "ann@x.org", phone: "555" }];
const got = broadcast({ channels: [e, s] }, users, { subject: "A long subject for a text", body: "" });
equal(got, ["Ann: email,sms"], "broadcast");
equal(e.sent, ["ann@x.org: A long subject for a text"], "email sent");
equal(s.sent, ["555: A long subject for a"], "sms sent");
