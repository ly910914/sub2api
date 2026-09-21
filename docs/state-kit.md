# STATE ticket integration

This fork integrates the source edition of
[wangyunjeff/sub2api-state-kit](https://github.com/wangyunjeff/sub2api-state-kit)
at commit `31420adaaf2b29ea1ad6bafd108c268b7b8df441`.
It includes the supporting Sub2API ticket changes from commit
`49a39b6dc1abed30fd227611e8af1108bc427610`, adapted to this fork.
See [attribution](../STATE-KIT-NOTICE.md).

## Configuration

1. Build and restart the backend with the updated frontend assets.
2. Open **System Settings > Gateway > Codex**, enable STATE and configure the
   global dynamic proxy pool with your own HTTP or SOCKS5 URL. Proxy credentials
   are not returned by the settings API. Saving an empty URL preserves the
   existing pool; use the clear control to remove it.
3. Edit an active OpenAI OAuth account. Save its business proxy selection first
   (a fixed proxy or no proxy for a direct server connection), then reopen it.
4. In **STATE**, select Pro (292 bytes) or Team (332 bytes), enable the account
   and save. Acquisition starts automatically. The same panel shows status,
   renewal progress, expiration, errors and the manual reacquisition control.
5. Clients continue to use the existing API key and base URL.

The master switch and per-account settings default to disabled. No schema
migration is needed: global configuration uses settings, and private account
configuration and tickets use `accounts.extra`. Do not simultaneously enable
the independent STATE Kit plugin for these accounts.

## Behavior

- Dynamic proxies only acquire candidates. Each candidate is verified again
  through the account's business route before publication.
- Proxy usernames may contain `{sid}` for a new session per attempt. The
  `1024proxy.io` username SID format is also supported.
- The optional startup setting
  `gateway.openai_codex_ticket.harvest_dial_proxy_url` adds an outer HTTP CONNECT
  proxy for acquisition only.
- Tickets last one hour and renew ten minutes before expiry. A failed renewal
  preserves a still-valid ticket and its original expiration.
- Acquisition makes at most eight attempts and has an eight-minute total
  deadline, including account authentication. Each failed attempt is shown in
  the account panel while retries continue. Logs include safe failure summaries
  distinguishing acquisition from business-route verification; credentials and
  STATE contents are never included. Failed rounds cool down for five minutes.
- An enabled account without a valid ticket is excluded from scheduling for its
  configured target model. Other models and accounts keep their normal paths.
  Disabling the account's STATE switch restores its normal routing.
- Managed WebSocket sessions use the existing HTTP bridge, checking the current
  ticket on each turn. Enabling STATE on a native session requires reconnecting.
- A completed response with a mismatched model, or the experimental 312-byte
  STATE signal, invalidates the ticket and schedules recovery. Business requests
  are never replayed by this observer.
- APIs, account export/import and audit records redact private ticket fields.
  The database still contains the private STATE and proxy configuration.

The source edition provides account settings and status; the plugin's separate
proxy testing and model demonstration page is not included. Actual acquisition
requires your own account and dynamic proxy pool. Matching response model fields
does not guarantee model quality, availability or concurrency.
