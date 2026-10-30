---
name: Personal Activity
description: Summarize the user's activity day with the personalactivity CLI.
---

Use this skill when the user asks for a summary, timeline, report, or question about their personal activity.

## Default workflow

The default request is to summarize the user's day. Run:

```sh
personalactivity --format json
```

This collects all available activity sources from local midnight until now. Read the JSON, group related activity, and give the user a concise factual summary. Mention useful dates, topics, and outcomes; do not dump every raw event unless the user asks for a timeline or details.

## Filters

Tune the command before collecting when the request gives a narrower scope:

- Use `--from` and `--to` for a requested date or time range. Dates accept `YYYY-MM-DD`; timestamps accept RFC3339.
- Use one or more `--source` options when the user explicitly asks about a particular provider or wants to exclude providers.
- Always use `--format json` when analysing results. Use the default table output only when the user wants a terminal-friendly listing.
- Run `personalactivity --help` when the available flags or source selection syntax is unclear.

Do not hard-code or assume a list of event sources. A plain `personalactivity --format json` includes every configured source, including sources added in the future. Only select sources when the user's request requires it.

## Reporting

- State the time range used when it differs from today.
- Distinguish observed activity from inference.
- For `thunderbird` events, distinguish sent emails (`attributes.message_id`) from calendar events (`attributes.event_id`, `attributes.calendar`, `attributes.start`/`attributes.end`, `attributes.location`, `attributes.organizer`, `attributes.attendees`), and filter out irrelevant shared/free-busy calendars when summarizing the user's own work.
- For `firefox` events (`topic` = host, `attributes.url`, `attributes.title`, `attributes.description`), group related browsing visits by domain/topic or task rather than listing every individual page load, and omit routine/background noise unless relevant.
- If no events match, say so plainly and suggest widening the date range or removing a source filter.
- Keep raw command text, commit messages, prompts, email bodies, meeting descriptions, and full URLs (especially query parameters/tokens) to the minimum needed for the requested summary; they can contain sensitive details.
