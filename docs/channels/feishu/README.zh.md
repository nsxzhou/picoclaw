# 飞书

飞书（国际版名称：Lark）是字节跳动旗下的企业协作平台。它通过事件驱动的 Webhook 同时支持中国和全球市场。

## 配置

```json
{
  "channels": {
    "feishu": {
      "enabled": true,
      "app_id": "cli_xxx",
      "app_secret": "xxx",
      "encrypt_key": "",
      "verification_token": "",
      "allow_from": []
    }
  }
}
```

| 字段                  | 类型   | 必填 | 描述                                   |
| --------------------- | ------ | ---- | -------------------------------------- |
| enabled               | bool   | 是   | 是否启用飞书频道                       |
| app_id                | string | 是   | 飞书应用的 App ID(以cli\_开头)         |
| app_secret            | string | 是   | 飞书应用的 App Secret                  |
| encrypt_key           | string | 否   | 事件回调加密密钥                       |
| verification_token    | string | 否   | 用于Webhook事件验证的Token             |
| allow_from            | array  | 否   | 用户ID白名单，空表示所有用户           |
| random_reaction_emoji | array  | 否   | 随机添加的表情列表，空则使用默认 "Pin" |

## 设置流程

1. 前往 [飞书开放平台](https://open.feishu.cn/)创建应用程序
2. 获取 App ID 和 App Secret
3. 配置事件订阅和Webhook URL
4. 设置加密(可选,生产环境建议启用)
5. 将 App ID、App Secret、Encrypt Key 和 Verification Token(如果启用加密) 填入配置文件中
6. 自定义你希望 PicoClaw react 你消息时的表情（可选, Reference URL: [Feishu Emoji List](https://open.larkoffice.com/document/server-docs/im-v1/message-reaction/emojis-introduce))

## 文档能力扩展（Feishu Docs MCP）

如果你希望 PicoClaw 在飞书会话里直接读写飞书文档，可启用内置 MCP sidecar：

```json
{
  "tools": {
    "mcp": {
      "enabled": true,
      "servers": {
        "feishu-doc": {
          "enabled": true,
          "command": "picoclaw",
          "args": ["mcp-feishu-doc", "serve"]
        }
      }
    }
  }
}
```

权限建议（飞书开放平台）：

1. 文档读取与导出：`docs:doc`
2. 文档创建/编辑：`docx:document` 相关读写权限
3. 云空间文件与权限管理：`drive:*` 中与你要使用的能力对应权限（评论、分享、成员权限、删除）

说明：

1. 该 sidecar 使用 `channels.feishu.app_id/app_secret` 鉴权。
2. 写操作采用二次确认（先返回 `action_id`，再携带确认语重试）。
3. 审计日志默认写入工作区 `state/feishu_doc_audit.jsonl`。
4. MCP 工具名前缀示例：`mcp_feishu-doc_doc_read`。
