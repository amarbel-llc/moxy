import { $ } from "zx";

$.verbose = false;
$.stdio = ["pipe", "pipe", "ignore"];

const [daysStr, calendar, timezone] = process.argv.slice(2);

const days = Number(daysStr) || 3;
const now = new Date();
const end = new Date(now.getTime() + days * 24 * 60 * 60 * 1000);

const p: Record<string, unknown> = {
  calendarId: calendar || "primary",
  timeMin: now.toISOString(),
  timeMax: end.toISOString(),
  singleEvents: true,
  orderBy: "startTime",
  maxResults: 50,
};

if (timezone) {
  p.timeZone = timezone;
}

const params = JSON.stringify(p);
const result = await $`gws calendar events list --params ${params}`;

process.stdout.write(
  JSON.stringify({
    content: [{ type: "text", text: result.stdout, mimeType: "application/json" }],
  }),
);
