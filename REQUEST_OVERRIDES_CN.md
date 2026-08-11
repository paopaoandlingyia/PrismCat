# 请求覆盖规则

PrismCat 请求覆盖是一套小型的项目专用 DSL，用于在请求发往上游前修改请求。它并不兼容 RFC 6902 JSON Patch：PrismCat 使用 GJSON/SJSON 点号路径，操作语义也不同。

## 生效条件

一条规则只有在以下三层开关都开启时才会运行：

1. 全局启用请求覆盖。
2. 上游启用参数覆盖，并通过规则 `name` 绑定该规则。
3. 规则自身设置为 `enabled: true`。

规则名称同时是绑定标识，必须非空且唯一。规则按照上游绑定列表中的顺序执行；每条请求体规则看到的是前序规则修改后的请求体。

## 规则结构

```json
{
  "name": "Default max_tokens",
  "enabled": true,
  "match": {
    "methods": ["POST"],
    "path_prefixes": ["/v1/"],
    "paths": [],
    "json": [
      { "path": "model", "starts_with": "gpt-" }
    ]
  },
  "patch": [
    { "op": "default", "path": "max_tokens", "value": 4096 }
  ],
  "headers": [
    { "op": "set", "name": "X-Example", "value": "example" }
  ]
}
```

不需要时可以省略 `methods`、`path_prefixes`、`paths`、`json`、`patch` 和 `headers`。

## 匹配条件

- `methods`：忽略大小写的精确匹配，保存时会规范为大写。
- `paths`：区分大小写的完整请求路径匹配。
- `path_prefixes`：区分大小写的请求路径前缀匹配。
- `json`：使用 GJSON 路径判断请求体内容。

不同匹配组之间是 AND；`methods`、`paths`、`path_prefixes` 各自数组内部是 OR；`json` 数组中的每个条件都必须成立。

JSON 条件支持：

- `exists: true`：路径必须存在。
- `equals`：JSON 值必须与给定值深度相等。
- `starts_with`：值必须是以指定内容开头的字符串。
- `in`：JSON 值必须与给定数组中的一个成员深度相等。

每条规则执行前都会重新判断 JSON 条件，因此能看到前序请求体规则造成的修改。

> 当前限制：暂不支持 `exists: false`。在“匹配字段不存在”得到实现前，请省略该条件或改用其他正向条件。

## 请求体操作

请求体操作只支持未压缩的 JSON 请求（`application/json` 或以 `+json` 结尾的媒体类型）。请求体会在 `max_body_bytes` 限制内缓冲。任何 patch 操作失败都会返回 HTTP 400，不会把部分修改后的请求发送给上游。

读取路径遵循 [GJSON 语法](https://github.com/tidwall/gjson/blob/master/SYNTAX.md)，写入路径遵循 [SJSON 路径语法](https://github.com/tidwall/sjson#path-syntax)。

| 操作 | 行为 |
| --- | --- |
| `set` | 设置或覆盖值；SJSON 会创建缺失的父级对象或数组。 |
| `remove` | 删除指定路径的值。 |
| `default` | 仅当路径不存在时设置值。 |
| `append` | 向数组末尾追加一个值；路径不存在时创建单元素数组，已有值不是数组时出错。 |
| `prepend` | 向数组开头插入一个值；路径不存在时创建单元素数组，已有值不是数组时出错。 |

同一规则中的操作按照数组顺序依次执行。

## 请求头操作

请求头支持：

| 操作 | 行为 |
| --- | --- |
| `set` | 将请求头替换为一个值。 |
| `remove` | 删除请求头。 |

请求头操作只判断 `methods`、`paths` 和 `path_prefixes`，不会判断 `match.json`，因为它无需解析或缓冲请求体。这也意味着纯请求头规则可以应用于非 JSON 或压缩请求。

认证信息应视为密钥。PrismCat 会在日志和普通规则预览中隐藏已配置的敏感请求头，但保存的配置文件与高级原始 JSON 编辑器仍包含真实值。

## 安全示例

下面的示例默认停用且未绑定：

```json
{
  "name": "Example: default max_tokens",
  "enabled": false,
  "match": {
    "methods": ["POST"]
  },
  "patch": [
    { "op": "default", "path": "max_tokens", "value": 4096 }
  ]
}
```

启用规则前，请先将它绑定到目标上游，并通过 PrismCat 日志核对原始请求与最终请求的差异。
