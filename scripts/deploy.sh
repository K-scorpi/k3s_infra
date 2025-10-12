#!/bin/bash
set -e  # Остановить при первой ошибке

KUBER_DIR="./kuber"
APPS_DIR="./apps"

usage() {
  echo "Usage: $0 [all|cert-manager|homer|minio|velero|bot|kilo|check]"
  exit 1
}

apply_dir() {
  local dir="$1"
  local name="$2"
  echo "🚀 Applying $name from $dir"
  kubectl apply -k "$dir" 2>/dev/null || kubectl apply -f "$dir"
}

case "${1:-all}" in
  cert-manager)
    apply_dir "$KUBER_DIR/cert-manager" "cert-manager"
    ;;
  homer)
    apply_dir "$KUBER_DIR/homer" "Homer dashboard"
    ;;
  minio)
    apply_dir "$KUBER_DIR/minio" "MinIO"
    ;;
  velero)
    apply_dir "$KUBER_DIR/velero" "Velero"
    ;;
  bot)
    apply_dir "$APPS_DIR/go-bot" "Telegram bot"
    ;;
  kilo)
    apply_dir "$KUBER_DIR/kilo" "Kilo (WireGuard mesh)"
    ;;
  ingress)
    apply_dir "$KUBER_DIR/ingress" "Ingress configs"
    ;;
  upgrader)
    apply_dir "$KUBER_DIR/upgrader" "System Upgrade Controller"
    ;;
  all)
    echo "📦 Deploying full stack..."
    $0 cert-manager
    sleep 5
    $0 kilo
    $0 ingress
    $0 homer
    $0 minio
    $0 velero
    $0 bot
    $0 upgrader
    echo "✅ All done!"
    ;;
  check)
    echo "🔍 Validating Kubernetes manifests..."
    if ! command -v kubeval &> /dev/null; then
      echo "❌ kubeval not found. Install it: https://github.com/instrumenta/kubeval"
      exit 1
    fi
    while IFS= read -r -d '' file; do
      echo "✅ Validating $file"
      kubeval --strict --ignore-missing-schemas "$file"
    done < <(find . -name "*.yaml" \
               -not -path "./secret/*" \
               -not -path "./velero/credentials-velero" \
               -not -path "*/kgctl-*" \
               -not -name "*values.yaml" \
               -not -name "*k3s.yaml" \
               -not -name "*peer*.yaml" \
               -print0)
    echo "✅ All manifests passed dry-run validation"
    ;;
  *)
    usage
    ;;
esac


kubectl create secret generic telegram-bot-secret-2 \
  -n bots \
  --from-literal=token='8341782773:AAEB6unmAxqveRwBSHLVGLBVetoT74wn4Z4' \
  --from-literal=chat_id='1202444249'