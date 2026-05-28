Run the test suite for this project.

- Default: `make test` (unit + integration + e2e, excludes smoke)
- If the user specifies a layer, run only that layer:
  - unit → `go test ./test/unit/... -v`
  - integration → `go test ./test/integration -v`
  - e2e → `go test ./test/e2e -v`
  - smoke → `go test ./test/smoke -v` (requires live server at SMOKE_BASE_URL)
- If the user specifies a package or test name, run with `-run <pattern>` scoped to the relevant path.
- After running, report: pass/fail count, any failures with the relevant output, and whether coverage dropped (run with `-cover` if not already).
- If tests fail, identify the root cause before suggesting fixes.
