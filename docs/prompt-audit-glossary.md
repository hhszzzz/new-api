# 提示词审查术语表

| 术语 | 含义 |
| --- | --- |
| 提示词审查（Prompt audit） | 对客户端提交的完整文本上下文执行独立安全分类的网关能力。 |
| 敏感词检查（Sensitive-word check） | 既有的关键词检查；优先于提示词审查执行，语义不变。 |
| 完整上下文（Full client context） | 客户端提交的消息、角色文本和顶层指令；不包含工具定义、metadata、二进制、加密推理或网关注入内容。 |
| 最新用户段优先（Latest-user-first） | 首先审查最新非空 `user` 文本，再按原请求顺序审查其余文本。 |
| 审查节点（Audit node） | 提供 OpenAI 兼容接口并运行 Qwen3Guard 的有序故障切换端点。 |
| 输入限制（Input limit） | 单次节点调用可接收的 Unicode 字符数；所有启用节点中的最小值决定分片大小。 |
| 分片重叠（Chunk overlap） | 相邻文本分片重复审查的小范围字符，用于降低边界切分绕过风险。 |
| Pass | 安全放行，不触发拦截。 |
| Flag | 告警放行；异步或同步记录风险，但不阻断请求。 |
| Block | 阻断；同步模式返回 `prompt_audit_blocked`。 |
| Unavailable | 审查无法可靠完成；同步模式失败关闭并返回 `prompt_audit_unavailable`。 |
| `would_action` | 异步模式下记录如果采用同步策略本应执行的 Pass、Flag、Block 或 Unavailable。 |
| 配置版本（Config version） | 覆盖模式、类别、分组、节点和运行参数的指纹，用于记录与缓存隔离。 |
| 脱敏预览（Redacted preview） | 列表和无明文权限详情中显示的不可还原短预览。 |
| 扫描载荷（Scan payload） | 异步任务处理所需的临时完整文本；任务终态后清除。 |
| 留存原文（Retained full prompt） | 仅授权管理员可查看的审计副本，最多 65,536 个 Unicode 字符；留存期到期后只清除该明文，不删除判定元数据。 |
| 租约（Lease） | Worker 对异步任务的限时所有权；过期任务可被其他实例原子回收。 |
| 失败关闭（Fail closed） | 同步审查不能确定安全结果时拒绝请求，而不是默认放行。 |
| 删除高水位（Delete high-water mark） | 删除预览返回的最大记录 ID；确认时用于防止把预览后新产生的记录误删。 |
