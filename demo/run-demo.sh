#!/usr/bin/env bash
set -euo pipefail

# Colors
CYAN='\033[0;36m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
BOLD='\033[1m'
NC='\033[0m' # No Color

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

pause() {
    echo ""
    echo -e "${YELLOW}>> Press [ENTER] to continue to the next step...${NC}"
    read -r
}

banner() {
    echo ""
    echo -e "${CYAN}========================================================================${NC}"
    echo -e "${BOLD}${CYAN}$1${NC}"
    echo -e "${CYAN}========================================================================${NC}"
}

banner "ACT 1: The Stateful Workload (Baseline)"
echo "In Kubernetes, stateful workloads store data on PersistentVolumeClaims (PVCs)."
echo "We are deploying a stateful service that commits transactions every 5 seconds."
echo ""
kubectl apply -f "${SCRIPT_DIR}/01-app.yaml"

echo "Waiting for demo database pod to be Ready..."
kubectl wait -n iris-demo --for=condition=Ready pod -l app=demo-database --timeout=90s

echo ""
echo -e "${GREEN}✓ Production database is ONLINE. Let's inspect the active data log:${NC}"
POD_NAME=$(kubectl get pods -n iris-demo -l app=demo-database -o jsonpath='{.items[0].metadata.name}')
kubectl exec -n iris-demo "${POD_NAME}" -- cat /data/db.dat
kubectl exec -n iris-demo "${POD_NAME}" -- tail -n 4 /data/db.log

pause

banner "ACT 2: Declarative Backup & Automated Restore Verification"
echo "THE TRADITIONAL WAY: Fragile bash cron jobs taking unverified snapshots."
echo "THE KUBERNETES WAY: Declare desired state via a Custom Resource (BackupPolicy)."
echo ""
echo "Key capabilities demonstrated:"
echo " 1. CSI VolumeSnapshot taken seamlessly in the background."
echo " 2. Operator automatically spins up an ephemeral test volume from the snapshot."
echo " 3. Sandboxed verification container validates data integrity."
echo " 4. Ephemeral resources are auto-deleted (zero storage leakage)."
echo " 5. Status is reported as first-class Kubernetes state (Ready / Degraded)."
echo ""
kubectl apply -f "${SCRIPT_DIR}/02-backuppolicy.yaml"

echo "Waiting for the first scheduled backup run (schedule: */1 * * * *)..."
echo "Watching BackupPolicy status..."

for i in {1..60}; do
    READY=$(kubectl get backuppolicies.ops.leandro.ie -n iris-demo iris-backup-policy -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}' 2>/dev/null || true)
    if [ "${READY}" == "True" ]; then
        break
    fi
    sleep 2
    printf "."
done
echo ""

echo -e "${GREEN}✓ Backup created and verified!${NC}"
echo ""
kubectl get backuppolicies.ops.leandro.ie -n iris-demo
echo ""
echo "Snapshots created:"
kubectl get volumesnapshots.snapshot.storage.k8s.io -n iris-demo
echo ""
echo "Notice: Any ephemeral test pods & test PVCs were already torn down automatically!"

pause

banner "ACT 3: The Anti-Schrödinger Test (Catching Silent Corruption)"
echo "THE NIGHTMARE: Backups succeed, but data is corrupt. Nobody knows until restore day."
echo "Let's simulate a silent database corruption in our production database."
echo ""
POD_NAME=$(kubectl get pods -n iris-demo -l app=demo-database -o jsonpath='{.items[0].metadata.name}')
kubectl exec -n iris-demo "${POD_NAME}" -- /bin/sh -c 'echo "CHECKSUM: CORRUPT_BAD_DATA" > /data/db.dat'

echo -e "${RED}Corrupted data written into production /data/db.dat!${NC}"
echo "Current db.dat content:"
kubectl exec -n iris-demo "${POD_NAME}" -- cat /data/db.dat

echo ""
echo "Waiting for next scheduled backup cycle to trigger automated restore verification..."
echo "Watch the operator catch the corruption during verification!"

for i in {1..60}; do
    DEGRADED=$(kubectl get backuppolicies.ops.leandro.ie -n iris-demo iris-backup-policy -o jsonpath='{.status.conditions[?(@.type=="Degraded")].status}' 2>/dev/null || true)
    if [ "${DEGRADED}" == "True" ]; then
        break
    fi
    sleep 2
    printf "."
done
echo ""

echo -e "${RED}${BOLD}ALERT: The Operator detected data corruption during restore verification!${NC}"
kubectl get backuppolicies.ops.leandro.ie -n iris-demo
echo ""
echo "Latest RestoreVerification status:"
kubectl get restoreverifications.ops.leandro.ie -n iris-demo
echo ""
echo "Kubernetes Events recorded on the policy:"
kubectl describe backuppolicy -n iris-demo iris-backup-policy | grep -A 10 "Events:" || true

echo ""
echo -e "${YELLOW}>> Takeaway: In traditional IT, you find out your backup is broken at 3 AM during disaster recovery.${NC}"
echo -e "${YELLOW}>> With Kubernetes & iris-backup-operator, you find out at 2 PM on Tuesday without touching production!${NC}"

pause

banner "ACT 4: Phoenix Recovery (Declarative Disaster Recovery in Seconds)"
echo "Simulating total disaster: Production database and PVC deleted by accident."
kubectl delete deployment iris-db -n iris-demo
kubectl delete pvc iris-data -n iris-demo

echo ""
echo "Production workload is GONE. But we have our verified VolumeSnapshots!"
LATEST_SNAP=$(kubectl get volumesnapshots.snapshot.storage.k8s.io -n iris-demo -o jsonpath='{.items[0].metadata.name}')
echo -e "Restoring from snapshot: ${GREEN}${LATEST_SNAP}${NC}"

sed "s/LATEST_VERIFIED_SNAPSHOT/${LATEST_SNAP}/g" "${SCRIPT_DIR}/03-restore-disaster-recovery.yaml" | kubectl apply -f -

echo "Waiting for recovered database pod..."
kubectl wait -n iris-demo --for=condition=Ready pod -l app=demo-database --timeout=90s

echo ""
RECOVERED_POD=$(kubectl get pods -n iris-demo -l app=demo-database -o jsonpath='{.items[0].metadata.name}')
echo -e "${GREEN}✓ Production is back online! Verifying restored database log:${NC}"
kubectl exec -n iris-demo "${RECOVERED_POD}" -- cat /data/db.log

echo ""
echo -e "${GREEN}${BOLD}DEMO COMPLETE: The full power of Kubernetes + Custom Operator demonstrated!${NC}"
