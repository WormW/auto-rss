---
phase: 01-bug
plan: 07
type: execute
wave: 1
depends_on: []
files_modified:
  - internal/repository/download.go
  - internal/repository/subscription.go
  - internal/repository/config.go
  - internal/repository/log.go
autonomous: true
requirements:
  - SEC-02
must_haves:
  truths:
    - 所有 GORM Where 子句使用参数化查询
    - 无动态 SQL 字符串拼接
    - 无 GORM Raw() 或 Exec() 的字符串拼接
  artifacts:
    - path: internal/repository/download.go
      provides: "安全的 GORM 查询"
      contains: 'Where("status = ?"'
    - path: internal/repository/subscription.go
      provides: "安全的 GORM 查询"
      contains: 'Where("status = ?"'
  key_links:
    - from: repository methods
      to: GORM parameterized queries
      pattern: 'Where\("[^"]*\?"'
---

<objective>
审计 repository 层的所有 GORM 查询，确保所有 Where 子句使用参数化查询，无动态 SQL 拼接，无 Raw() 或 Exec() 的字符串拼接。

Purpose: 确认代码库中不存在 SQL 注入漏洞。虽然使用 GORM，但需验证所有用户输入都正确参数化。
Output: 完整的 SQL 注入审计报告，所有发现的问题已修复或确认安全。
</objective>

<execution_context>
@$HOME/.claude/get-shit-done/workflows/execute-plan.md
@$HOME/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/phases/01-bug/01-CONTEXT.md
@internal/repository/download.go
@internal/repository/subscription.go
@internal/repository/config.go
@internal/repository/log.go

<!-- Decisions D-16, D-17, D-18: -->
- D-16: 重点审计动态条件查询（List 的 status 过滤等）
- D-17: 确认使用 GORM 参数化查询：`Where("status = ?", status)`
- D-18: 检查是否有 `Raw()` 或 `Exec()` 的字符串拼接

<!-- Current download.go Where clauses: -->
```go
// Line 75: Where("torrent_hash = ?", hash)
// Line 88: Where("status = ?", status)
// Line 104: Where("subscription_id = ? AND episode = ?", subscriptionID, episode)
// Line 115: Where("subscription_id = ? AND episode = ?", subscriptionID, episode)
// Line 123: Where("subscription_id = ?", subscriptionID)
// Line 131: Where("subscription_id = ?", subscriptionID)
// Line 138: Where("id = ?", id).Update("status", status)
// Line 152: Where("status = ?", status)
// Line 157: Where("1 = 1") // Safe — no user input
// Line 166: Where("status = ? AND retry_count < max_retries AND (next_retry_at IS NULL OR next_retry_at <= ?)", "failed", now)
// Line 178: Where("retry_count >= ? AND retry_count <= ?", minRetries, maxRetries)
```

<!-- Current subscription.go Where clauses: -->
```go
// Line 58: Where("rss_url = ?", rssURL)
// Line 89: Where("status = ?", "active")
```

<!-- Need to check other repository files too -->
</context>

<tasks>

<task type="auto">
  <name>Task 1: Audit all repository files for SQL injection risks</name>
  <files>internal/repository/*.go</files>
  <read_first>
    - internal/repository/download.go (full file)
    - internal/repository/subscription.go (full file)
    - internal/repository/config.go (full file)
    - internal/repository/log.go (full file)
    - internal/repository/rss_source_repository.go (full file)
  </read_first>
  <action>
Audit all repository files for SQL injection vulnerabilities. For each file, check:

1. **download.go**: Already reviewed — all Where clauses use `?` placeholders. No issues found.
   - Confirm: `Where("torrent_hash = ?", hash)` ✓
   - Confirm: `Where("status = ?", status)` ✓
   - Confirm: `Where("subscription_id = ? AND episode = ?", subscriptionID, episode)` ✓
   - Confirm: `Where("status = ? AND retry_count < max_retries AND (next_retry_at IS NULL OR next_retry_at <= ?)", "failed", now)` ✓
   - No `Raw()` or `Exec()` calls found ✓

2. **subscription.go**: Already reviewed — all Where clauses use `?` placeholders. No issues found.
   - Confirm: `Where("rss_url = ?", rssURL)` ✓
   - Confirm: `Where("status = ?", "active")` ✓
   - No `Raw()` or `Exec()` calls found ✓

3. **config.go**: Read and audit. Check for:
   - Any `Where` clauses with string concatenation
   - Any `Raw()` or `Exec()` calls
   - Any dynamic query building

4. **log.go**: Read and audit. Same checks.

5. **rss_source_repository.go**: Read and audit. Same checks.

For any file found with issues, fix by converting to parameterized queries.

If any repository uses `Raw()` or `Exec()` with string building, replace with parameterized versions.

If any `Where` clause uses string concatenation (e.g., `Where("status = '" + status + "'")`), replace with `Where("status = ?", status)`.

Create a summary document of the audit findings.
  </action>
  <verify>
    <automated>grep -rn "Raw(" internal/repository/*.go || echo "No Raw() calls found"</automated>
    <automated>grep -rn "Exec(" internal/repository/*.go | grep -v "\.Error" || echo "No problematic Exec() calls"</automated>
    <automated>grep -rn 'Where(".*+' internal/repository/*.go || echo "No string concatenation in Where"</automated>
    <automated>grep -rn 'Where(".*\?' internal/repository/*.go | wc -l</automated>
  </verify>
  <acceptance_criteria>
    - All `internal/repository/*.go` files audited
    - Zero `Raw()` calls with user-input strings found
    - Zero `Exec()` calls with string concatenation found
    - Zero `Where()` calls with string concatenation found
    - All `Where()` clauses use `?` parameter placeholders
    - Audit report documents findings for each file
  </acceptance_criteria>
  <done>
    - All repository files audited for SQL injection risks
    - All queries confirmed to use parameterization
    - Any issues found are documented and fixed
    - Project builds successfully
  </done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| User input -> Database | All user input passes through repository layer to GORM |

## STRIDE Threat Register

| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-07-01 | Injection | Repository Where clauses | mitigate | All queries use `?` placeholders; no dynamic SQL |
| T-07-02 | Information Disclosure | SQL errors | accept | GORM error messages are generic; no raw SQL exposed |
</threat_model>

<verification>
- `go build ./...` succeeds
- `grep -rn "Raw(" internal/repository/*.go` returns empty or only safe usages
- `grep -rn 'Where(".*+' internal/repository/*.go` returns empty
- Every `Where` clause uses `?` placeholder
</verification>

<success_criteria>
- SEC-02 resolved: All SQL queries pass injection audit
- 100% of Where clauses use parameterization
- Zero Raw() or Exec() calls with string concatenation
- Audit report created documenting all findings
</success_criteria>

<output>
After completion, create `.planning/phases/01-bug/01-07-audit-sql-injection-SUMMARY.md`
</output>
