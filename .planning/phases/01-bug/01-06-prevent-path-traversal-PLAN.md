---
phase: 01-bug
plan: 06
type: execute
wave: 1
depends_on: []
files_modified:
  - internal/api/handler/subscription.go
  - internal/service/organizer/organizer.go
autonomous: true
requirements:
  - SEC-01
must_haves:
  truths:
    - 用户输入的路径无法跳出允许目录
    - 允许合理的子目录结构
    - 路径规范化后必须以前缀开头
  artifacts:
    - path: internal/api/handler/subscription.go
      provides: "路径输入验证"
      contains: "filepath.Clean"
      contains2: "strings.HasPrefix"
    - path: internal/service/organizer/organizer.go
      provides: "整理器路径保护"
      contains: "ValidatePath"
  key_links:
    - from: subscription handler
      to: ValidatePath function
      pattern: "ValidatePath"
---

<objective>
实现路径遍历防护。使用 filepath.Clean 规范化用户输入路径，验证最终路径是否以允许的根目录为前缀，阻止 ../ 序列跳出目录。

Purpose: 当前用户输入构造路径未充分校验，攻击者可能使用 ../ 序列访问系统任意文件。
Output: 所有用户输入路径都经过验证，无法跳出指定目录。
</objective>

<execution_context>
@$HOME/.claude/get-shit-done/workflows/execute-plan.md
@$HOME/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/phases/01-bug/01-CONTEXT.md
@internal/api/handler/subscription.go
@internal/service/organizer/organizer.go

<!-- Path construction in subscription.go (line 421): -->
```go
// 生成带番剧名的下载路径
downloadPath := utils.GenerateDownloadPath(savePath, subscription.Name)
```

<!-- Path construction in organizer.go: -->
```go
fullPath := filepath.Join(f.destDir, newFileName)
```

<!-- Decisions D-13, D-14, D-15: -->
- D-13: 路径规范化：使用 `filepath.Clean()` 清理用户输入路径
- D-14: 前缀检查：验证最终路径是否以允许的根目录为前缀
- D-15: 允许合理的子目录（如 `Anime/Action/`），不只是禁止 `..`

<!-- Note: subscription.go doesn't directly accept user paths in APIs currently. -->
<!-- The main path construction happens via config (download_path) and generated paths. -->
<!-- However, subscription.CollectionTorrent URL could potentially be manipulated. -->
<!-- The organizer processes files from watchDir and moves to destDir — both from config. -->

<!-- Actually, looking at subscription.go more carefully, there isn't direct user path input. -->
<!-- The paths come from: -->
<!-- 1. Config "download_path" -->
<!-- 2. Generated paths from subscription name -->
<!-- 3. Collection torrent URLs -->

<!-- The real path traversal risk is in organizer.go where files are moved. -->
<!-- But the decision says both subscription.go and organizer.go need protection. -->

<!-- Let's focus on: -->
<!-- 1. Any path inputs from user/API -->
<!-- 2. File operations in organizer -->
</context>

<tasks>

<task type="auto">
  <name>Task 1: Create path validation utility and apply to handlers</name>
  <files>internal/api/handler/subscription.go, internal/service/organizer/organizer.go</files>
  <read_first>
    - internal/api/handler/subscription.go (look for any path-related user input)
    - internal/service/organizer/organizer.go (watchDir, destDir usage)
    - internal/pkg/utils/ directory (check if ValidatePath exists)
  </read_first>
  <action>
Create a path validation utility and apply it to both files:

**Step 1: Create path validation function**

Add to `internal/pkg/utils/path.go` (create if doesn't exist):

```go
package utils

import (
    "fmt"
    "path/filepath"
    "strings"
)

// ValidatePath checks that the given path is within the allowed root directory.
// It uses filepath.Clean for normalization and strings.HasPrefix for the containment check.
// Returns the cleaned path or an error if the path escapes the root.
func ValidatePath(path, allowedRoot string) (string, error) {
    // Clean both paths
    cleanedPath := filepath.Clean(path)
    cleanedRoot := filepath.Clean(allowedRoot)

    // Ensure the root ends with a separator for proper prefix checking
    if !strings.HasSuffix(cleanedRoot, string(filepath.Separator)) {
        cleanedRoot += string(filepath.Separator)
    }

    if !strings.HasPrefix(cleanedPath+string(filepath.Separator), cleanedRoot) {
        return "", fmt.Errorf("path %q escapes allowed root %q", path, allowedRoot)
    }

    return cleanedPath, nil
}

// ValidatePathOrDefault validates a path, returning the default if validation fails
func ValidatePathOrDefault(path, allowedRoot, defaultPath string) string {
    validated, err := ValidatePath(path, allowedRoot)
    if err != nil {
        return defaultPath
    }
    return validated
}
```

**Step 2: Apply validation in subscription handler**

In `internal/api/handler/subscription.go`, the main path usage is in `downloadCollectionTorrent` where it constructs `downloadPath`. The `savePath` comes from `h.downloadPath` (constructor parameter). Add validation:

At the beginning of `downloadCollectionTorrent`:
```go
// Validate download path
if h.downloadPath != "" {
    if _, err := utils.ValidatePath(h.downloadPath, "/downloads"); err != nil {
        logger.Error("Invalid download path configured",
            "download_path", h.downloadPath,
            "error", err)
        return
    }
}
```

Actually, the allowed root should come from config or be a constant. Let's use a package-level constant. But wait — the download path IS configurable by the user via config. The validation should check against a configurable base path.

Better approach: validate any path constructed from user input. In subscription.go, the only user-provided path-like input is the `collection_torrent` URL. That's a URL, not a filesystem path.

The actual path traversal risk in subscription.go is minimal since paths are config-driven. But let's add defensive validation for the download path used in file operations.

In `doCollectEpisodes`, `doReorganizeFiles`, `doRenameFiles` — they all use `h.downloadPath` and construct paths. Add validation at the point where paths are used for file operations.

Actually, let's take a different approach per D-13/D-14/D-15. The main risk is:
1. User-provided subscription name could contain `../` and be used in path construction
2. User-provided file paths in any API

The subscription name IS user-provided. It's used in `utils.GenerateDownloadPath(basePath, subscription.Name)`. If the name contains `../`, it could escape the base path.

So the fix should be:
1. Sanitize subscription names before using them in paths
2. Validate any path that includes user input

Let me revise:

**Step 1: Create path validation utility** (same as above)

**Step 2: Sanitize subscription name in path generation**

In `utils.GenerateDownloadPath`, ensure the subscription name is sanitized. But we don't have access to that function's source right now. Let's add validation at the call sites.

In subscription.go, before calling `utils.GenerateDownloadPath`:
```go
// Validate the generated path doesn't escape the base
downloadPath := utils.GenerateDownloadPath(savePath, subscription.Name)
if validatedPath, err := utils.ValidatePath(downloadPath, savePath); err != nil {
    logger.Error("Generated path escapes base directory",
        "subscription", subscription.Name,
        "path", downloadPath,
        "error", err)
    return // or handle error
} else {
    downloadPath = validatedPath
}
```

**Step 3: Apply in organizer.go**

In `organizeFile`, validate the generated `newPath`:
```go
// Validate the target path doesn't escape the destination directory
if validatedPath, err := utils.ValidatePath(newPath, f.destDir); err != nil {
    return fmt.Errorf("generated path escapes destination directory: %w", err)
} else {
    newPath = validatedPath
}
```

Also validate `filePath` (the source) to ensure it's within `f.watchDir`:
```go
if _, err := utils.ValidatePath(filePath, f.watchDir); err != nil {
    return fmt.Errorf("source file path escapes watch directory: %w", err)
}
```

This prevents symlink attacks or path manipulation from the watch directory.

**Summary of changes:**

1. Create `internal/pkg/utils/path.go` with `ValidatePath` function
2. In subscription.go: validate paths generated from subscription names
3. In organizer.go: validate both source and destination paths

For the organizer.go changes, the validation should be added:
- At the start of `organizeFile`: validate `filePath` is within `f.watchDir`
- Before moving: validate `newPath` is within `f.destDir`
  </action>
  <verify>
    <automated>go build ./...</automated>
    <automated>grep -n "ValidatePath" internal/pkg/utils/path.go</automated>
    <automated>grep -n "ValidatePath" internal/api/handler/subscription.go</automated>
    <automated>grep -n "ValidatePath" internal/service/organizer/organizer.go</automated>
  </verify>
  <acceptance_criteria>
    - `internal/pkg/utils/path.go` exists with `func ValidatePath(path, allowedRoot string) (string, error)`
    - `ValidatePath` calls `filepath.Clean(path)` and `filepath.Clean(allowedRoot)`
    - `ValidatePath` uses `strings.HasPrefix` with separator suffix for proper containment check
    - `subscription.go` calls `utils.ValidatePath` before using generated download paths
    - `organizer.go` validates `filePath` against `f.watchDir` at the start of `organizeFile`
    - `organizer.go` validates `newPath` against `f.destDir` before file move
    - Paths containing `../` that would escape the root return an error
    - Valid subdirectories like `Anime/Action/` are allowed
  </acceptance_criteria>
  <done>
    - Path validation utility created
    - Subscription handler validates generated paths
    - Organizer validates source and destination paths
    - Path traversal via `../` is blocked
    - Valid subdirectories are allowed
    - Project builds successfully
  </done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| User input -> File system | User-provided names/IDs used to construct file paths |

## STRIDE Threat Register

| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-06-01 | Elevation of Privilege | Path construction | mitigate | `filepath.Clean` + `strings.HasPrefix` ensures path containment |
| T-06-02 | Tampering | Symlink attacks | accept | Basic path validation; full symlink protection out of scope for this phase |
</threat_model>

<verification>
- `go test ./internal/pkg/utils/...` passes (if tests added)
- `go build ./...` succeeds
- `grep "ValidatePath" internal/pkg/utils/path.go` returns a match
</verification>

<success_criteria>
- SEC-01 resolved: Path traversal attacks are prevented
- User input paths are normalized with filepath.Clean
- Prefix check ensures paths stay within allowed directory
- Valid subdirectories are permitted
</success_criteria>

<output>
After completion, create `.planning/phases/01-bug/01-06-prevent-path-traversal-SUMMARY.md`
</output>
