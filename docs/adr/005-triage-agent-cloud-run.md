# ADR 005: Triage Agent Deployment on Cloud Run

## Status
Proposed

## Context
The triage agent needs to run on a weekly schedule to scan ConduitIO/conduit
issues and produce a ranked task queue. Cloud Run + Cloud Scheduler is the
target deployment model per the architecture proposal.

## Decision
Deploy the triage agent as a Cloud Run service triggered by Cloud Scheduler.

### Deployment Path
1. **ADK Go built-in**: `adkgo deploy cloudrun` handles cross-compilation,
   Dockerfile generation, and `gcloud run deploy`.
2. **Manual**: Custom Dockerfile in `cmd/triage/Dockerfile` for more control
   (e.g., including the `gh` CLI).

### Secrets Required
- `GOOGLE_API_KEY` — Gemini API access (Cloud Run secret)
- `GH_TOKEN` — GitHub API access for `gh` CLI (Cloud Run secret)

### Scheduling
Cloud Scheduler triggers the Cloud Run service weekly:
```
gcloud scheduler jobs create http triage-weekly \
  --schedule="0 9 * * 1" \
  --uri="https://triage-HASH-uc.a.run.app/api/run" \
  --http-method=POST \
  --body='{"message":"Run triage on open issues"}' \
  --oidc-service-account-email=triage-invoker@PROJECT.iam.gserviceaccount.com
```

### Cost Estimate
| Component | Monthly Cost |
|-----------|-------------|
| Gemini 2.5 Flash (4 runs × ~100K tokens) | $0.08 |
| Cloud Run (4 invocations × ~60s each) | Free tier |
| Cloud Scheduler (1 job) | Free tier (3 free jobs) |
| **Total** | **~$0.08/month** |

## Consequences
- Near-zero cost for weekly triage
- GitHub access requires a PAT or GitHub App token stored as a secret
- The `gh` CLI must be included in the container image
- Cloud Run cold starts may add 5-10s to first invocation
