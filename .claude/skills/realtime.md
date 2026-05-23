## Realtime Event Rules

- Typing events are ephemeral; never persist in DB unless analytics requires it.
- Use TTL-based expiration (ex: 2–5s).
- Debounce frequent typing updates.
- Avoid per-keystroke network sends.
- Broadcast only to room participants.
- Handle stale sessions after disconnect.
- Support idempotent event handling.
- Prefer server-authoritative presence state.
- Separate message events from transient events.