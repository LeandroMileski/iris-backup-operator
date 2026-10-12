# iris-backup-operator tasks

Tick a box only after its acceptance check has evidence.

Current step: **0.2**. Tools are installed in the Multipass VM `op-dev`. No kind cluster yet.

## Phase 0: Environment

- [x] 0.1 Verify tools: `go`, `kubebuilder`, `docker`, `kind`, `kubectl`, `make`, `helm`. Report versions; install nothing without asking.
  - Done in `op-dev`: go 1.27.2, kubebuilder v4.16.0, docker 29.1.3, kind v0.33.0, kubectl v1.37.1, make 4.3, helm v3.22.0.
- [ ] 0.2 Create a kind cluster with the snapshot CRDs, snapshot controller, and CSI hostpath driver. Document the exact steps in `docs/dev-setup.md`.
  - Accept: a test PVC can be snapshotted manually with a `VolumeSnapshot` and reaches `readyToUse: true`.

## Phase 1: Scaffold and API

- [ ] 1.1 `kubebuilder init` and create the API (`BackupPolicy`, then `RestoreVerification`). Commit the untouched scaffold.
- [ ] 1.2 Define the `BackupPolicy` spec and status types, with validation markers and printer columns (schedule, last backup, ready).
- [ ] 1.3 Run `make generate manifests` and sample CRs in `config/samples/`.
  - Accept: `make test` passes, CRD installs with `make install`, invalid samples are rejected by the API server.

## Phase 2: Backup reconciler

- [ ] 2.1 Reconciler skeleton: fetch policy, handle not-found, set `observedGeneration`, set an initial `Ready` condition. Unit test with fake client.
- [ ] 2.2 Schedule logic as a pure function (`nextRun(schedule, last, now) (time.Time, error)`), table-driven tests including invalid cron and clock edge cases.
- [ ] 2.3 When a run is due, list PVCs from `targetSelector` and create a `VolumeSnapshot` per PVC with labels and an owner reference. Return `RequeueAfter` until the next run.
- [ ] 2.4 Retention: delete snapshots beyond `retention`, oldest first. Test with envtest.
- [ ] 2.5 Finalizer: on policy deletion, clean up owned snapshots, then remove the finalizer.
- [ ] 2.6 Status updates: conditions, `lastSuccessfulBackupTime`, Events for create/prune/failure. Use the status subresource and avoid update conflicts.
  - Accept: envtest integration tests cover create, retention, suspend, and delete paths.

## Phase 3: Verified restore

- [ ] 3.1 `RestoreVerification` controller skeleton and state machine (`Pending -> Running -> Succeeded|Failed`).
- [ ] 3.2 Restore: create a PVC from the snapshot (`dataSource`), then a verification pod mounting it read-only, running `verification.command` with a timeout.
- [ ] 3.3 Interpret the pod result (exit code) into `.status`, then clean up the pod and PVC (always, even on failure).
- [ ] 3.4 Policy controller creates a `RestoreVerification` after a successful backup when `verifyRestore` is true, and mirrors the latest result into `BackupPolicy.status.lastVerification`.
  - Accept: e2e on kind: break the verification command and see `Degraded`; fix it and see recovery.

## Phase 4: Admission webhooks

- [ ] 4.1 Defaulting webhook (`retention`, `schedule` default, `suspend`).
- [ ] 4.2 Validating webhook: cron syntax, `retention >= 1`, non-empty selector, verification fields required when `verifyRestore` is true.
- [ ] 4.3 Webhook certificates via cert-manager. Unit tests for validation logic as plain functions.
  - Accept: invalid policies are rejected with clear messages; valid ones get defaults.

## Phase 5: Security

- [ ] 5.1 Review generated RBAC. Reduce to the minimum verbs and resources actually used, and justify each rule in `docs/rbac.md`.
- [ ] 5.2 Verification pods: non-root, read-only root filesystem, dropped capabilities, `seccompProfile: RuntimeDefault`, no service account token, resource limits.
- [ ] 5.3 `NetworkPolicy` that denies all traffic for verification pods. Prove it with a test.
- [ ] 5.4 Operator image: distroless, non-root, pinned base image digest.

## Phase 6: Observability

- [ ] 6.1 Metrics: `backup_last_success_timestamp_seconds`, `backup_runs_total{result}`, `restore_verify_duration_seconds`, `snapshots_pruned_total`.
- [ ] 6.2 Structured logging with consistent keys (policy name/namespace, snapshot, reconcile ID).
- [ ] 6.3 `ServiceMonitor`, Grafana dashboard JSON, and example alert rules (no successful backup in 26h, verification failed).
- [ ] 6.4 Health and readiness endpoints verified in the e2e test.

## Phase 7: Testing and CI

- [ ] 7.1 Coverage report. Target meaningful coverage of pure logic (schedule, retention, validation) over line-count chasing.
- [ ] 7.2 GitHub Actions: lint, unit + envtest, build image, e2e on kind.
- [ ] 7.3 Release workflow: tag, build and push image, package Helm chart, create GitHub release.

## Phase 8: Delivery

- [ ] 8.1 Helm chart for the operator (CRDs, RBAC, webhook, metrics), with a values schema.
- [ ] 8.2 Argo CD `Application` manifest deploying the chart, plus a sample `BackupPolicy`.
- [ ] 8.3 (Optional) Terraform for a small EKS cluster to run the e2e demo. Keep it separate under `deploy/terraform/` and document cost and teardown.

## Phase 9: Documentation

- [ ] 9.1 README: problem, architecture diagram, quick start on kind, CRD reference.
- [ ] 9.2 `docs/runbook.md`: backup stuck, snapshots not ready, verification failing, webhook cert problems. Use real failure output captured during testing.
- [ ] 9.3 `docs/design.md`: key decisions and trade-offs (why snapshots, why a child CR for verification, why no vendored types).
- [ ] 9.4 Short demo recording or asciinema, linked from the README.
