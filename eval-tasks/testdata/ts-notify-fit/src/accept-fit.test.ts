import * as notify from "./notify.ts";
import { equal } from "./testing.ts";

const s = new notify.SMS();
equal(s.fit("short"), "short", "fit(short)");
equal(s.fit("a subject that is longer than twenty"), "a subject that is lo", "fit of a long one");
s.send({ id: "u1", name: "Ann", email: "", phone: "5" }, { subject: "a subject that is longer than twenty", body: "" });
equal(s.sent, ["5: a subject that is lo"], "sms sent");
