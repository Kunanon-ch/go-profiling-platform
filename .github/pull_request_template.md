## Summary

Describe the change and why it is needed.

## Validation

- [ ] `gofmt` reports no changed files.
- [ ] `go mod tidy` leaves `go.mod` and `go.sum` unchanged.
- [ ] `go vet ./...` passes.
- [ ] `go test -race ./...` passes.
- [ ] `go test -tags smoke ./cmd/pyroscope-smoke` passes.
- [ ] If the module path changed, `GOWORK=off go run ./cmd/set-module-path -- <path>` was used and `GOWORK=off go list -m` reports the intended path.
- [ ] Tests cover new or changed behavior.
- [ ] Changed Compose/Helm references render successfully and remain pinned.
- [ ] Documentation and `CHANGELOG.md` are updated when user-visible behavior changes.
- [ ] No credentials, tokens, private endpoints, or customer data are included.
- [ ] New images/dependencies passed vulnerability, provenance, and license review.

## Operational impact

- Rollout or migration required: <!-- yes/no, then explain -->
- Rollback approach: <!-- explain -->
- Security or IAM impact: <!-- explain -->
- Performance impact: <!-- explain or state not expected -->
