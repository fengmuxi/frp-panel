# FAQ

- Q: How to disable the remote shell feature of a node?

  A: In the `.env` file of `frp-panel`, set `CLIENT_FEATURES_ENABLE_REMOTE_SHELL` to `false`.

- Q: How to disable the functions feature of a node?

  A: In the `.env` file of `frp-panel`, set `CLIENT_FEATURES_ENABLE_FUNCTIONS` to `false`.

- Q: There are two records in the `clients` / `servers` tables that look like dirty data. Can I delete them?

  A: **No — they are internal framework structures. Do NOT delete them** (see issue #172):

  1. **The record in `clients` with `is_shadow = 1` and `server_id = ''`**: it is the "shadow index" of a bare client id, referencing all of its `@n` child instances. When the client pulls its configuration, it uses this record to **recursively** pull the real config of each child instance. Deleting it causes `record not found` errors — the client can no longer pull configuration, and tunnels cannot self-heal after a disconnect.
  2. **The record in `servers` with `server_id = 'default'` and `user_id = 0`**: it is the master process's own server identity (`default`). The master pulls its own configuration every 30 seconds; deleting this record makes the master log `record not found` continuously.

  Both records have an empty `config_content`, `user_id = 0` and `server_id = ''`, so they look exactly like dirty data. If you see them in the panel lists, that is expected.