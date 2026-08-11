# Request Override Rules

PrismCat request overrides are a small, project-specific DSL for changing an
outgoing request before it reaches an upstream. They are not RFC 6902 JSON
Patch: PrismCat uses GJSON/SJSON dot paths and has different operation
semantics.

## Activation model

A rule runs only when all three switches are active:

1. Request overrides are enabled globally.
2. The upstream enables overrides and binds the rule by its `name`.
3. The rule itself has `enabled: true`.

Rule names are binding identifiers. Keep them non-empty and unique. Rules run
in the order listed by the upstream binding, and each body rule sees the body
produced by earlier rules.

## Rule shape

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

`methods`, `path_prefixes`, `paths`, `json`, `patch`, and `headers` may be
omitted when unused.

## Match conditions

- `methods`: case-insensitive exact matches. Values are normalized to uppercase.
- `paths`: case-sensitive exact request-path matches.
- `path_prefixes`: case-sensitive request-path prefix matches.
- `json`: body conditions evaluated with GJSON paths.

Different match groups are combined with AND. Values inside `methods`, `paths`,
or `path_prefixes` are alternatives (OR). Every item in `json` must match.

A JSON condition supports:

- `exists: true`: the path must exist.
- `exists: false`: the path must not exist.
- `equals`: the JSON value must deeply equal the supplied value.
- `starts_with`: the value must be a string with this prefix.
- `in`: the value must deeply equal one member of the supplied array.

Body conditions are evaluated immediately before each rule, so they see
changes made by earlier body rules.

## Body operations

Body operations require an uncompressed JSON request (`application/json` or a
media type ending in `+json`). The request body is buffered up to
`max_body_bytes`. A patch error rejects the request with HTTP 400 rather than
sending a partially modified body upstream.

Paths use [GJSON syntax](https://github.com/tidwall/gjson/blob/master/SYNTAX.md)
for reads and [SJSON path syntax](https://github.com/tidwall/sjson#path-syntax)
for writes.

| Operation | Behavior |
| --- | --- |
| `set` | Sets or replaces the value. Missing parent objects or arrays are created by SJSON. |
| `remove` | Deletes the value at the path. |
| `default` | Sets the value only when the path does not exist. |
| `append` | Appends one value to an array. Creates a one-item array when the path is missing; errors when the existing value is not an array. |
| `prepend` | Prepends one value to an array. Creates a one-item array when the path is missing; errors when the existing value is not an array. |

Operations run sequentially in array order.

## Header operations

Header rules support:

| Operation | Behavior |
| --- | --- |
| `set` | Replaces the header with one value. |
| `remove` | Removes the header. |

Header operations use only `methods`, `paths`, and `path_prefixes`. They do
not evaluate `match.json`, because headers are applied without parsing or
buffering the request body. This also lets header-only rules work for non-JSON
and compressed requests.

Treat authorization values as secrets. PrismCat masks configured sensitive
headers in logs and in the normal rule preview, but the saved configuration and
advanced raw JSON editor contain the real values.

## Safe example

The example below is disabled and unbound by default:

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

Before enabling a rule, bind it to the intended upstream and verify the
original/final request diff in PrismCat logs.
