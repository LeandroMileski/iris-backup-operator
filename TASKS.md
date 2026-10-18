# iris-backup-operator tasks

Tick a box only after its acceptance check has evidence.

Current step: **3.4**. Policy controller creates a `RestoreVerification` after a successful backup when `verifyRestore` is true, and mirrors the latest result into `BackupPolicy.status.lastVerification`.

## Phase 0: Environment

- [x] 0.1 Verify tools: `go`, `kubebuilder`, `docker`, `kind`, `kubectl`, `make`, `helm`. Report versions; install nothing without asking.
  - Done in `op-dev`: go 1.27.2, kubebuilder v4.16.0, docker 29.1.3, kind v0.33.0, kubectl v1.37.1, make 4.3, helm v3.22.0.
- [x] 0.2 Create a kind cluster with the snapshot CRDs, snapshot controller, and CSI hostpath driver. Document the exact steps in `docs/dev-setup.md`.
  - Accept: a test PVC can be snapshotted manually with a `VolumeSnapshot` and reaches `readyToUse: true`.

## Phase 1: Scaffold and API

- [x] 1.1 `kubebuilder init` and create the API (`BackupPolicy`, then `RestoreVerification`). Commit the untouched scaffold.
- [x] 1.2 Define the `BackupPolicy` spec and status types, with validation markers and printer columns (schedule, last backup, ready).
  - Spec: `targetSelector`, `schedule`, `retention`, `suspend`, `snapshotClassName`, `verifyRestore`, `verification`.
  - Status: `lastSuccessfulBackupTime`, `lastVerification`, conditions `Ready` and `Degraded`.
  - `kubectl get backuppolicies` columns: Schedule, Last Backup, Ready. `make manifests generate` and `make test` passed in `op-dev`.
- [x] 1.3 Run `make generate manifests` and sample CRs in `config/samples/`.
  - Accept: `make test` passes, CRD installs with `make install`, invalid samples are rejected by the API server.
  - `make test` passed in `op-dev`. `make install` configured both CRDs on `kind-iris-dev`. `kubectl apply -k config/samples/` created `backuppolicy-sample` and `restoreverification-sample`.
  - `backuppolicy-invalid` (`schedule: not-a-cron`, `retention: 0`) was rejected: retention must be >= 1, schedule must match `^(\S+\s+){4}\S+$`. The object was not stored.

## Phase 2: Backup reconciler

- [x] 2.1 Reconciler skeleton: fetch policy, handle not-found, set `observedGeneration`, set an initial `Ready` condition. Unit test with fake client.
  - `make test` passed in `op-dev` (`internal/controller` coverage 83.3%). Fake-client tests cover a missing policy, a fetch error, `status.observedGeneration`, and `Ready=True` with reason `Reconciled`. A second reconcile of the same generation does not write status. A newer generation updates both fields. The envtest reconciler test asserts the same status.
- [x] 2.2 Schedule logic as a pure function (`nextRun(schedule, last, now) (time.Time, error)`), table-driven tests including invalid cron and clock edge cases.
  - `make test` passed in `op-dev` (`internal/controller` coverage 88.9%). `TestNextRun` covers the first run, an exact boundary, one second before and after midnight, a missed interval, a future `last`, UTC normalization including a daylight-saving wall clock, leap day, day 31, the year boundary, weekdays, and step schedules. Invalid cases include an empty schedule, non-cron text, out-of-range fields, the wrong field count, a zero step, `@daily`, February 31, and a zero `now`.
- [x] 2.3 When a run is due, list PVCs from `targetSelector` and create a `VolumeSnapshot` per PVC with labels and an owner reference. Return `RequeueAfter` until the next run.
  - `make test` passed in `op-dev` (`internal/controller` coverage 82.0%). A due policy snapshots only matching PVCs in its namespace. Each VolumeSnapshot carries policy, PVC, and schedule labels, an owner reference, and `volumeSnapshotClassName` when the policy sets one. A second pass over the same slot does not create another snapshot. Suspend, an invalid schedule, a selector with no PVCs, and a create error are covered. `status.lastSuccessfulBackupTime` is set to the run time so the next reconcile waits until the following slot.
- [x] 2.4 Retention: delete snapshots beyond `retention`, oldest first. Test with envtest.
  - `make test` passed in `op-dev` (`internal/controller` coverage 81.2%). An envtest policy with `retention: 2` keeps the two newest owned snapshots for a PVC and deletes the older one. A PVC under the limit is left alone, and a snapshot owned by another policy is left alone. Equal creation times are ordered by the scheduled instant, then by name. A missing `retention` uses 7.
- [x] 2.5 Finalizer: on policy deletion, clean up owned snapshots, then remove the finalizer.
  - `make test` passed in `op-dev` (`internal/controller` coverage 79.2%). Envtest adds `ops.leandro.ie/backup-policy` on reconcile. Deleting the policy removes the owned VolumeSnapshot, leaves a snapshot owned by another policy, and removes the finalizer so the policy itself is deleted.
- [x] 2.6 Status updates: conditions, `lastSuccessfulBackupTime`, Events for create/prune/failure. Use the status subresource and avoid update conflicts.
  - Accept: envtest integration tests cover create, retention, suspend, and delete paths.
  - `make test` passed in `op-dev` (`internal/controller` coverage 80.3%). One status-subresource write sets `observedGeneration`, `Ready`, `Degraded`, and `lastSuccessfulBackupTime` after the backup work. A conflicting write is retried against a fresh object. A due envtest policy creates a VolumeSnapshot, records `lastSuccessfulBackupTime`, sets `Ready=True` and `Degraded=False` with reason `Reconciled`, and emits `SnapshotCreated`. A suspended policy creates no snapshot, sets both conditions to `Suspended`, and leaves `lastSuccessfulBackupTime` empty. Retention emits `SnapshotPruned` for the deleted snapshot. Deleting the policy still removes its owned snapshot and the finalizer. A create or prune error sets `Ready=False` and `Degraded=True` with reason `BackupFailed`, does not advance `lastSuccessfulBackupTime`, and emits a warning event.

## Phase 3: Verified restore

- [x] 3.1 `RestoreVerification` controller skeleton and state machine (`Pending -> Running -> Succeeded|Failed`).
  - `make test` passed in `op-dev` (`internal/controller` coverage 81.8%). The spec names the VolumeSnapshot, the source claim, and the verification command. An empty phase becomes `Pending` and requeues; the next reconcile becomes `Running` and records `startTime`. A second reconcile of `Running` does not write status. `Succeeded` and `Failed` stay as recorded. `advanceVerification` moves `Running` to `Succeeded` or `Failed`, and rejects finishing `Pending` or changing a terminal phase. `Ready` is True only for `Succeeded`. `Degraded` is True only for `Failed`. A conflicting status write is retried. A missing object is ignored, and an unknown phase returns an error.
- [x] 3.2 Restore: create a PVC from the snapshot (`dataSource`), then a verification pod mounting it read-only, running `verification.command` with a timeout.
  - `make test` passed in `op-dev` (`internal/controller` coverage 81.5%). A `Pending` verification does not create objects. Entering `Running` creates an owned PersistentVolumeClaim whose `dataSource` is the VolumeSnapshot, copying the source claim's access mode, storage request, and storage class. It then creates an owned Pod with `restartPolicy: Never`, `activeDeadlineSeconds` from `timeoutSeconds` (600 when unset), and the verification command. The Pod mounts that claim read-only at `/data`. A later reconcile leaves both objects in place. A missing snapshot, a missing source claim, or a source claim without a storage request returns an error and does not create the Pod.
- [x] 3.3 Interpret the pod result (exit code) into `.status`, then clean up the pod and PVC (always, even on failure).
  - `make test` passed in `op-dev` (`internal/controller` coverage 80.5%). A Pod that is still running leaves the verification `Running`. Exit code 0 sets `Succeeded`, `Ready=True`, and `completionTime`. A non-zero exit code sets `Failed` and `Degraded=True`. A deadline sets `Failed` with a timeout message. The status message records the exit code. After the result is stored, the verification Pod and the restored PersistentVolumeClaim are deleted. The source claim and the VolumeSnapshot stay. An object that is still terminating counts as deleted; if the delete does not stick, the reconcile tries again after 5 seconds. A result that was already recorded still deletes a leftover Pod and claim.
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
- [ ] 7.2 GitHub Actions PR CI workflow (`.github/workflows/ci.yaml`):
  - Linting (`golangci-lint`, `go vet`).
  - Manifest & codegen drift verification (`make manifests generate` with `git diff --exit-code`) to prevent desynchronized CRDs.
  - Unit and integration tests with `envtest` (`make test`).
  - Ephemeral Kind cluster E2E run (`make test-e2e`) verifying real PVC, CSI snapshot, and pod execution.
- [ ] 7.3 Release workflow (`.github/workflows/release.yaml`):
  - Triggered on semantic tags (`v*.*.*`).
  - Multi-arch container image build (`linux/amd64`, `linux/arm64`) using Docker Buildx with pinned distroless base digest.
  - Push image to registry (e.g., GitHub Packages / GHCR).
  - Container image signing and provenance with Cosign.

## Phase 8: Delivery & GitOps (Argo CD)

- [ ] 8.1 GitOps directory structure (`deploy/`):
  - Operator packaging using native Kustomize bases (`config/default`), avoiding GitOps churn by adding CI path exclusions (`paths-ignore: ['deploy/**']`).
- [ ] 8.2 Argo CD `Application` manifest (`deploy/argocd/application.yaml`):
  - Deploy operator to `iris-backup-operator-system`.
  - Configure `ServerSideApply=true` to prevent CRD `last-applied-configuration` annotation size overflow (>262 KB limit).
  - Configure `CreateNamespace=true`, automated sync with `prune: true` and `selfHeal: true`.
  - Include sample `BackupPolicy` CR.
- [ ] 8.3 Argo CD Custom Lua Health Checks:
  - Add Lua health check script to `argocd-cm` ConfigMap for `ops.leandro.ie/BackupPolicy` and `RestoreVerification`.
  - Map `.status.conditions` (`Ready`, `Degraded`) to Argo CD health states (`Healthy`, `Degraded`, `Progressing`).
- [ ] 8.4 Automated image tag promotion:
  - Configure automated semver tag detection and Git write-back (via `argocd-image-updater` or CI release step).
- [ ] 8.5 (Optional) Terraform for a small EKS cluster to run the e2e demo. Keep it separate under `deploy/terraform/` and document cost and teardown.

## Phase 9: Documentation

- [ ] 9.1 README: problem, architecture diagram, quick start on kind, CRD reference.
- [ ] 9.2 `docs/runbook.md`: backup stuck, snapshots not ready, verification failing, webhook cert problems. Use real failure output captured during testing.
- [ ] 9.3 `docs/design.md`: key decisions and trade-offs (why snapshots, why a child CR for verification, why no vendored types).
- [ ] 9.4 Short demo recording or asciinema, linked from the README.

## Future Improvements

- [ ] Write a Helm chart for the operator (`deploy/helm/iris-backup-operator` via `kubebuilder edit --plugins=helm/v2-alpha`) when you need to distribute the operator to third parties or need custom values overrides across external users. For current internal GitOps delivery, native Kustomize (`config/default`) deployed via Argo CD is sufficient and reduces templating maintenance overhead.
- [ ] Dedicated GitOps repository: Migrate from single-repo `deploy/` directory to an isolated GitOps control repository (`iris-gitops-infra`) once multi-cluster or multi-team environment promotions (dev -> staging -> prod) require segregated access controls.

