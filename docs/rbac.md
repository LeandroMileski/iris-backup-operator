# RBAC

The controller ServiceAccount is `controller-manager`. `config/default` binds it to `manager-role` (cluster-wide) and `leader-election-role` (in the operator namespace). `config/rbac/role.yaml` is generated from the `+kubebuilder:rbac` markers. Regenerate it with `make manifests`. controller-gen folds rules that use the same verbs into one rule, so the YAML has fewer entries than the table.

Verbs below are the ones the reconcilers and the manager process call. A cache informer needs `list` and `watch`. A direct read needs `get`. Status is written with `Status().Update`, which is `update` on the status subresource.

## manager-role

| Rule | Verbs | Why it is used |
| --- | --- | --- |
| `backuppolicies` | get, list, watch, update | The policy controller watches BackupPolicy. Reconcile reads one object. Adding and removing `ops.leandro.ie/backup-policy` is an update of that object. |
| `backuppolicies/finalizers` | update | Kubernetes authorizes a finalizer change on this subresource as well as on the object update. |
| `backuppolicies/status` | update | `persistStatus` writes conditions, `observedGeneration`, and `lastSuccessfulBackupTime` through the status subresource. |
| `restoreverifications` | get, list, watch, create | The policy controller creates one owned RestoreVerification per snapshotted PVC and lists them to mirror `status.lastVerification`. The verification controller watches the same objects. |
| `restoreverifications/status` | update | `persistVerification` writes the phase, times, and conditions through the status subresource. |
| `persistentvolumeclaims` | get, list, watch, create, delete | The policy controller lists claims that match `targetSelector`. The verification controller watches claims, reads the source claim, creates the restored claim, and deletes it after the check. |
| `pods` | get, list, watch, create, delete | The verification controller watches Pods, creates the verification Pod, reads its exit code, and deletes it afterwards. |
| `volumesnapshots.snapshot.storage.k8s.io` | get, list, create, delete | The policy controller lists owned snapshots, creates one per PVC, and deletes snapshots past retention. The verification controller reads the snapshot named by the RestoreVerification. |
| `networkpolicies.networking.k8s.io` | get, create, update | The verification controller installs `iris-backup-verification-deny` in the verification namespace and restores it when its rules change. |
| `events` | create, patch | `Recorder.Event` writes a core v1 Event. client-go patches that Event to repeat it. Leader election records its Events through the same API. |

The policy controller does not create or delete BackupPolicy objects, and neither controller patches an object or its status. RestoreVerification has no finalizer. VolumeSnapshots are read and written during reconcile; nothing watches them. Events go to core v1, through `GetEventRecorderFor`, so there is no `events.k8s.io` rule.

## leader-election-role

The manager runs with `--leader-elect`. controller-runtime's default lock is `coordination.k8s.io/leases`, and the lease client only calls Get, Create, and Update. The role grants those three verbs. It does not grant ConfigMap access, and it does not grant `list`, `watch`, `patch`, or `delete` on leases. Election Events are covered by the `events` rule on `manager-role`.

## Metrics

Secure metrics are on by default (`--metrics-secure`). `metrics-auth-role` lets the manager call `tokenreviews` and `subjectaccessreviews` so it can authenticate and authorize a request to `/metrics`. `metrics-reader` grants `get` on the non-resource URL `/metrics` for whoever is bound to it. The controller ServiceAccount is not bound to `metrics-reader`.

## User roles

`backuppolicy-{admin,editor,viewer}-role` and `restoreverification-{admin,editor,viewer}-role` are installed for a cluster admin to bind to people. They are not referenced by the controller ServiceAccount, and the controller does not use them.
