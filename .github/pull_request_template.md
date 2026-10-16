## Description
Provide a clear description of the changes.

---

## Type of Change
- [ ] Bug fix
- [ ] New feature
- [ ] Refactor
- [ ] Documentation
- [ ] Test
- [ ] Breaking change

---

## Testing
- [ ] Unit tests added/updated
- [ ] All tests passing (`make test`)
- [ ] Manual testing performed (if required)

---

## Checklist

### Code Quality
- [ ] Code is properly formatted (`gofmt`)
- [ ] Linting passed (`make lint`)
- [ ] Vet passed (`make vet`)
- [ ] No debug logs / commented code

---

### Observability (IMPORTANT for your system)
- [ ] Logs include `trace_id` / `request_id`
- [ ] Errors are RFC 9457 problems (`c.Error` / `gincommon.AbortWithProblem` / `middleware.WriteProblem`)
- [ ] Middleware patterns followed

---

### Security
- [ ] No secrets hardcoded
- [ ] Input validation handled
- [ ] Authorization checked (if endpoint changes)

---

### Documentation
- [ ] README updated (if needed)
- [ ] API docs updated

---

## 🔗 Related Issue
Closes #<issue-id>

---

## Deployment Notes
Mention anything important for deployment.
