# Security policy

## Supported versions

| Version | Supported |
|---------|-----------|
| `v1.0.x` (current) | Yes — security fixes released as patch versions |
| `< v1.0.0` | No |

When a new major line (e.g. `v2`) ships, the previous major receives security fixes only for six months after the new major's release.

## Reporting a vulnerability

**Do not open a public GitHub issue for security vulnerabilities.**

Report security issues by email to **vijay@bcbpsolutions.com** with the subject line `[platform-gincommon] Security vulnerability`.

Include:

- A description of the vulnerability and its potential impact.
- Steps to reproduce or a proof-of-concept (a minimal Go test or curl sequence is sufficient).
- The version(s) affected.
- Any suggested fix, if you have one.

You will receive an acknowledgement within **2 business days** and a resolution timeline within **5 business days**.

## Trust model and known limitations

This library is designed to run **behind a trusted API gateway** that authenticates users and injects identity headers (`x-user-id`, `x-tenant-id`, `x-tenant-roles`). It validates header format but does **not** cryptographically verify identity.

| Assumption | Implication |
|-----------|-------------|
| API gateway strips client-supplied identity headers before forwarding | If a service is exposed directly (no gateway), any client can impersonate any user by setting `x-user-id` |
| `x-tenant-roles` is set by the gateway after authentication | If the gateway forwards client-supplied role headers, privilege escalation is possible |
| `NewRBAC()` is a reference implementation only | Do not use `NewRBAC()` in production; implement `port.Authorizer` with your IAM/OPA policy |
| `/metrics` is served without authentication in the example server | Restrict access to `/metrics` via network policy, an auth proxy, or a dedicated admin port in production |

## Scope

The following are in scope for vulnerability reports:

- Authentication bypass in `RequireAuth` middleware
- Header injection or log injection in `LoggingMiddleware` or `PanicRecoveryMiddleware`
- Sensitive data leakage in log output (fields not covered by `service.Sanitize`)
- Privilege escalation through `RBACService` or `AuthZMiddleware`
- Denial-of-service conditions in any middleware

Out of scope: theoretical attacks that require the attacker to already control the API gateway, issues in transitive dependencies unrelated to this library's functionality, or issues in the `cmd/server` demo binary.
