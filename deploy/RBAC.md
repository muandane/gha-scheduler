# gha-scheduler RBAC notes

## Secret `delete` permission

The Role grants `delete` on `secrets` in namespace `gha-runners`. SPEC §6 lists
`create/get/list/watch` as the minimum; we exceed that intentionally.

**Why:** [`internal/dispatch/dispatcher.go`](../../internal/dispatch/dispatcher.go)
creates the JIT `Secret` before the `Job`. If `Job` creation fails after the
`Secret` is created, dispatch deletes the orphan `Secret` so JIT blobs are not
left without a consuming Job.

Without `delete`, failed dispatches would leak Secrets until manual cleanup.

## Other verbs

- `secrets` `update`: patch owner reference onto Secret after Job UID is known.
- `leases` `create/update/delete`: per-job dispatch lock + reconciler leader election (SPEC §6, ARCHITECTURE §5).
