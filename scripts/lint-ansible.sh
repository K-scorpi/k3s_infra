#!/bin/bash
set -e

echo "🔍 Running Ansible linting..."

# Check if we're in the ansible directory
if [ -f "ansible.cfg" ]; then
    ANSIBLE_DIR="."
else
    ANSIBLE_DIR="ansible"
fi

echo "📁 Ansible directory: $ANSIBLE_DIR"

# Install tools if missing
if ! command -v yamllint &> /dev/null; then
    echo "📦 Installing yamllint..."
    pip install yamllint
fi

if ! command -v ansible-lint &> /dev/null; then
    echo "📦 Installing ansible-lint..."
    pip install ansible-lint
fi

# YAML lint
echo "🔍 Running yamllint..."
yamllint "$ANSIBLE_DIR" || true

# Ansible lint
echo "🔍 Running ansible-lint..."
if [ -f "$ANSIBLE_DIR/ansible.cfg" ]; then
    cd "$ANSIBLE_DIR"
    ansible-lint playbooks/ || true
    cd - > /dev/null
else
    ansible-lint "$ANSIBLE_DIR/playbooks/" || true
fi

# Playbook syntax check
echo "🔍 Validating playbook syntax..."
for playbook in "$ANSIBLE_DIR"/playbooks/*.yml; do
    if [ -f "$playbook" ]; then
        echo "✅ Checking: $(basename "$playbook")"
        ansible-playbook "$playbook" --syntax-check
    fi
done

echo "🎉 Linting completed!"