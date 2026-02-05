# GitHub Deployment Configuration Guide

To enable automated deployments to your Proxmox VM (`vm501`), you need to configure the following **Repository Secrets** in GitHub.

## 1. Navigate to Secrets
1. Go to your GitHub repository: [https://github.com/Vasteva/MediaConverter](https://github.com/Vasteva/MediaConverter)
2. Click **Settings** (top tab).
3. In the left sidebar, click **Secrets and variables** > **Actions**.
4. Click the green **New repository secret** button.

## 2. Add Secrets

Add the following three secrets:

### Secret 1: Server Host
- **Name:** `DEPLOY_HOST`
- **Secret:** `192.168.30.21`

### Secret 2: Deployment User
- **Name:** `DEPLOY_USER`
- **Secret:** `rwurtz`

### Secret 3: SSH Key
- **Name:** `SSH_PRIVATE_KEY`
- **Secret:** (Copy the entire content of your private key)

To get your private key content, run this command in your terminal:
```bash
cat ~/.ssh/id_ed25519
```

> **Note:** Copy the entire block including `-----BEGIN OPENSSH PRIVATE KEY-----` and `-----END OPENSSH PRIVATE KEY-----`.

## 3. Server Preparation (Completed)
The necessary files (`docker-compose.yml` and `.env`) have already been deployed to `/opt/vastiva` on the server. The server is ready to accept deployments.

## 4. Status Update (2026-02-05)
- Secrets `DEPLOY_HOST`, `DEPLOY_USER`, and `SSH_PRIVATE_KEY` have been configured.
- Server is prepped and `vastiva-test` container has been stopped to free up port 8091.
- Retrying deployment via CI/CD.
