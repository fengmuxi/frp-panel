// safe-parse.ts 容错 JSON 解析（全链路审计 2026-11-19 P2-J）：
// 单条脏配置（如高级模式保存了非法 JSON）此前会让代理列表/客户端卡片等
// 整页渲染崩溃（_app 无 ErrorBoundary）。统一走本入口：
// 解析失败返回 fallback，调用方按空配置兜底展示。

/** JSON.parse 容错版：失败返回 fallback */
export function parseJSONSafe<T>(raw: string | undefined | null, fallback: T): T {
  if (!raw) {
    return fallback
  }
  try {
    return JSON.parse(raw) as T
  } catch {
    return fallback
  }
}

/** JSON.parse 容错版：失败返回 null（调用方判空跳过渲染） */
export function parseJSONSafeOrNull<T>(raw: string | undefined | null): T | null {
  if (!raw) {
    return null
  }
  try {
    return JSON.parse(raw) as T
  } catch {
    return null
  }
}
