import { broadcast, Email, SMS } from "./notify.ts";
import { equal } from "./testing.ts";

const e = new Email();
const s = new SMS();
const users = [
  { id: "u1", name: "Ann", email: "a@x", phone: "5", optOut: { sms: true } },
  { id: "u2", name: "Bo", email: "b@x", phone: "6" },
];
const got = broadcast({ channels: [e, s] }, users, { subject: "s", body: "" });
equal(got, ["Ann: email", "Bo: email,sms"], "broadcast");
equal(e.sent, ["a@x: s", "b@x: s"], "email sent");
equal(s.sent, ["6: s"], "sms sent");
