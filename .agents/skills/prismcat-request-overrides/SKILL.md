---
name: prismcat-request-overrides
description: Create, review, or troubleshoot PrismCat request override rules, including match conditions, body operations, header overrides, upstream bindings, and safe handling of authorization values. Use for PrismCat rule JSON; do not use for unrelated proxy configuration.
---

# PrismCat request overrides

Read `../../../REQUEST_OVERRIDES.md` completely before creating or editing a rule.

When helping with a rule:

1. Ask for or infer the target upstream, request method/path, representative input JSON, and desired output.
2. Distinguish body changes from header changes. Never rely on `match.json` to constrain a header operation.
3. Prefer the smallest rule that expresses the requested behavior. Do not add operations or conditions without a stated need.
4. Give the rule a non-empty, unique name. Treat the name as a binding identifier and mention that renaming requires binding updates.
5. Use only documented PrismCat operations and GJSON/SJSON dot-path syntax. Do not describe the DSL as RFC 6902 JSON Patch.
6. Use placeholders such as `<API_KEY>` for secrets unless the user explicitly supplies and asks to store a value. Never echo an existing secret unnecessarily.
7. Return a single rule object by default, not a replacement array for the whole rule library.
8. Keep new examples disabled unless the user explicitly asks to activate them.
9. Explain the three activation layers: global switch, upstream binding, and rule switch.
10. Include a compact input-to-output example for non-trivial body rules and call out HTTP 400 failure cases.

Before finalizing, verify:

- the rule name is unique;
- method and path scope are narrow enough;
- body operations target an uncompressed JSON request;
- `append` and `prepend` target arrays or intentionally create them;
- header secrets use placeholders;
- the intended upstream binds the rule in the required execution order.
