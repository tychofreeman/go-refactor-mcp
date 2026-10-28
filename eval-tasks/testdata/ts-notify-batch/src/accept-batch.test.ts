import { broadcast, Email, type Batch } from "./notify.ts";
import { equal } from "./testing.ts";

const e = new Email();
const b: Batch = { users: [{ id: "u1", name: "Ann", email: "a@x", phone: "" }], message: { subject: "s", body: "" } };
const got = broadcast({ channels: [e] }, b);
equal(got, ["Ann: email"], "broadcast");
equal(e.sent, ["a@x: s"], "email sent");
