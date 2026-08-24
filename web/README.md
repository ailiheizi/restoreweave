# RestoreWeave browser client

This directory is part of the `v0.1.0-prealpha.1` core preview.

This is a small React/Vite client for the optional loopback API. It contains
no storage logic: all requests are `command.Envelope` calls to
`POST /api/v1/command`.

1. Enable the adapter in the persisted TOML profile:

```toml
[api]
enabled = true
listen = "127.0.0.1:4534"
```

2. Start `restoreweaved` and run `npm ci && npm run dev` in this
directory. Set `RESTOREWEAVE_API_TOKEN` when the API is not confined to a
trusted local process.

The browser is a convenience surface. It must not be required for exact
ingest, verification, or clean-install restore.
