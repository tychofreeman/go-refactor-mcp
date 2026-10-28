// Sends messages to users by email and text message.
import { log } from "./log.ts";

/** Someone to notify. */
export interface User {
  id: string;
  name: string;
  email: string;
  phone: string;
}

/** What is sent. */
export interface Message {
  subject: string;
  body: string;
}

/** Sends a message to a user on one channel. */
export interface Notifier {
  send(u: User, m: Message): void;
  name(): string;
}

/** Sends by email. */
export class Email implements Notifier {
  sent: string[] = []; // what was sent, "address: subject"

  /** Sends m to u's email address. */
  send(u: User, m: Message): void {
    log(`email to ${u.email}`);
    this.sent.push(u.email + ": " + m.subject);
  }

  /** The channel's name. */
  name(): string {
    return "email";
  }
}

/**
 * Sends by text message, which holds the subject only, cut to 20
 * characters.
 */
export class SMS implements Notifier {
  sent: string[] = []; // what was sent, "phone: subject"

  /** Sends m to u's phone. */
  send(u: User, m: Message): void {
    log(`sms to ${u.phone}`);
    this.sent.push(u.phone + ": " + truncate(m.subject, 20));
  }

  /** The channel's name. */
  name(): string {
    return "sms";
  }
}

function truncate(s: string, n: number): string {
  if (s.length <= n) {
    return s;
  }
  return s.slice(0, n);
}

/** Says how broadcast sends. */
export interface Config {
  channels: Notifier[];
}

/**
 * Sends m to each user on every channel, in the order given, and returns
 * a summary line for each user: "name: channel,channel".
 */
export function broadcast(c: Config, users: User[], m: Message): string[] {
  const out: string[] = [];
  for (const u of users) {
    const names: string[] = [];
    for (const ch of c.channels) {
      ch.send(u, m);
      names.push(ch.name());
    }
    out.push(`${u.name}: ${names.join(",")}`);
  }
  return out;
}

/** Sorts users in place by less, keeping the order of equal ones. */
export function sortUsers(users: User[], less: (a: User, b: User) => boolean): void {
  users.sort((a, b) => (less(a, b) ? -1 : less(b, a) ? 1 : 0));
}
