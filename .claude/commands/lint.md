Run the linter and fix all issues.

1. Run `make lint` (`go tool golangci-lint run`)
2. For each reported issue, apply the fix directly in the source file — do not just describe it.
3. After fixing, re-run `make lint` to confirm zero issues remain.
4. If a lint rule seems wrong for this codebase, flag it rather than suppressing with a nolint comment.
