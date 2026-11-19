# OpenShift OAuth/OIDC transition verification

Use this reference when pairing on authentication operator transitions. It is a scenario guide derived from an HA Service-migration verification, not a fixed sequence for every PR. Inspect the current implementation, feature gates, runbook, and cluster before selecting steps. Reads below are examples; mutations require the user's authorization.

## Baseline

Identify a build for the target SHA, record its immutable image digest, and verify cluster pull access before replacing an operator. Record relevant feature gates, topology, and authentication mode. If the authentication type is empty, confirm how the target release interprets it.

Useful baseline reads:

```bash
oc config current-context
oc get authentication cluster -o jsonpath='{.spec.type}{"\n"}'
oc get co authentication kube-apiserver console
oc -n openshift-oauth-apiserver get svc
oc get kubeapiserver cluster -o json |
  jq '{latestAvailableRevision: .status.latestAvailableRevision,
       nodes: [.status.nodeStatuses[] |
         {nodeName, currentRevision, targetRevision}]}'
```

Before authorized operator replacement or a rollout hold, preserve the relevant original deployment fields and ClusterVersion overrides. Merge temporary overrides with existing entries; do not overwrite unrelated overrides or restore a stale full object over concurrent changes. Plan how to undo only this run's changes.

## Paused-KAS Service migration

Use this sequence when the scenario calls for holding KAS revisions:

1. Begin with IntegratedOAuth and KAS revisions whose webhook targets the legacy `api` Service.
2. With authorization, mark the CKASO deployment unmanaged by CVO and scale CKASO to zero. This pauses operator-driven revisions; verify KAS itself remains available and its revisions remain unchanged.
3. With authorization, mark CAO unmanaged by CVO and replace both its container image and `OPERATOR_IMAGE` with the target image. Wait for rollout and leadership; check the running image digest.
4. Wait for the dedicated `token-review` Service, serving certificate, oauth-apiserver rollout, ready endpoints, and updated source webhook kubeconfig. Confirm running KAS still consumes the old revisioned destination.
5. Prepare a provider and Authentication manifest without applying the mode change prematurely. Existing Keycloak setup can be reused only after checking issuer, CA, audiences, claims, and client configuration. Never print provider credentials or tokens.
6. Start resource/deployment and KAS-revision monitors in separate terminals. With authorization, apply OIDC while CKASO is held.
7. Establish that legacy `api` and the OAuth/User APIServices remain and oauth-apiserver stays in standard mode while any current or pending KAS revision still requires the legacy webhook. A CAO restart during this hold is a separate, explicitly authorized scenario.
8. With authorization, restore CKASO's original replica count. Follow each current and target revision, legacy resource removal, and the subsequent switch to `external-oidc` deployment mode.
9. Verify fresh provider tokens authenticate, final operator health, and stable `token-review` UID/ClusterIP across the transition. Record restoration of operator images, replica counts, overrides, and other temporary changes according to the agreed desired final state.

The source webhook Secret being updated is an intermediate observation, not proof that KAS has adopted it. Discover all nonzero current and target revisions from KAS status and inspect their revisioned ConfigMaps and webhook Secrets.

Example extraction for one discovered revision, using the user's shell:

```bash
# Set revision to a revision observed in this run.
oc -n openshift-kube-apiserver get secret "webhook-authenticator-${revision}" \
  -o jsonpath='{.data.kubeConfig}' |
  base64 -d |
  rg '^[[:space:]]*(server|tls-server-name):'
```

Inspect revisioned KAS configuration as well to distinguish direct OIDC from webhook mode; absence of a webhook argument alone is insufficient. Match endpoint URL, path, and TLS identity against the actual dedicated Service.

## Additional scenarios

- **Direct OIDC to webhook OIDC:** establish direct OIDC before starting; watch that preparation does not recreate legacy OAuth resources or a standard OAuth deployment. Check replacement endpoint readiness before KAS switches, mixed direct/webhook revisions, fresh-token authentication, and final convergence.
- **Webhook OIDC to IntegratedOAuth:** watch restoration of legacy resources and standard deployment, verify IntegratedOAuth login and the expected certificate arrangement, and compare dedicated Service UID/ClusterIP before and after.
- **Partial removal or changed Service IP:** usually exercise through focused unit coverage first. Deliberately deleting live Services or revisioned resources is a separate recovery scenario requiring specific authorization and an understood recovery path. Recreating a Service does not guarantee recovery of its old IP.

## Evidence shape

A transition monitor can record timestamp, authentication type, presence of `api` and both APIServices, `token-review` UID/ClusterIP, deployment mode, and updated/ready/desired replicas. Record per-node current/target revisions alongside it. Treat read failures as unknown/error, not absent, and state the polling interval when assessing ordering.

Illustrative intermediate output:

```text
auth=OIDC api=present oauthAPI=present userAPI=present tokenReview=<UID>/<IP> mode=standard rollout=3/3/3
```

Record which revision first used `token-review`, when all KAS nodes adopted it, and when the legacy resources disappeared. Record deployment readiness and fresh-token authentication separately.

If the repository contains earlier verification evidence or monitor scripts, inspect them before reuse, particularly how the scripts handle read failures.
