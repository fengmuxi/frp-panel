# 常见问题

- Q: 如何关闭某个节点的远程 shell 功能？

  A: 在 `frp-panel` 的 `.env` 文件中，将 `CLIENT_FEATURES_ENABLE_REMOTE_SHELL` 设置为 `false` 即可。


- Q: 如何关闭某个节点的函数功能？

  A: 在 `frp-panel` 的 `.env` 文件中，将 `CLIENT_FEATURES_ENABLE_FUNCTIONS` 设置为 `false` 即可。

- Q: 数据库 `clients` / `servers` 表里有两条看着像"脏数据"的记录，可以删除吗？

  A: **不可以，它们是框架内部结构，请勿删除**（详见 issue #172）：

  1. **`clients` 表中 `is_shadow = 1` 且 `server_id = ''` 的记录**：这是裸 client id 的「影子索引」，记录了该节点下全部 `@n` 子实例。客户端拉取配置时会通过它**递归**拉取各子实例的真实配置。删除后 client 将报 `record not found`，再也拉不到配置，隧道断开后无法自愈。
  2. **`servers` 表中 `server_id = 'default'` 且 `user_id = 0` 的记录**：这是 master 进程自身的 server 身份（`default`），master 每 30 秒拉取一次自己的配置。删除后 master 日志会持续报 `record not found`。

  这两条记录的 `config_content` 为空、`user_id = 0`、`server_id = ''`，外观与脏数据一致，容易被误删；面板列表中如看到它们，属正常现象。