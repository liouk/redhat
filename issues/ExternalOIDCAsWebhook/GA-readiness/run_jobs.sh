#!/usr/bin/env bash

if ! command -v gangway-cli >/dev/null 2>&1; then
    echo "The command 'gangway-cli' could not be found. Please install 'gangway-cli' by cloning https://github.com/openshift-eng/gangway-cli and running 'go install'."
    exit 1
fi

if [[ -z "$MY_APPCI_TOKEN" ]]; then
    echo "No app.ci cluster token is set. A token for the app.ci cluster is required to run gangway-cli. Log in to https://console-openshift-console.apps.ci.l2s4.p1.openshiftapps.com/ and export your token to the 'MY_APPCI_TOKEN' environment variable."
    exit 1
fi

if [[ -z "$OCP_PAYLOAD" ]]; then
    echo "No payload set. Please select a payload from https://amd64.ocp.releases.ci.openshift.org/ and set 'OCP_PAYLOAD' to the one you've selected."
    exit 1
fi

RELEASE="5.1"
if [[ -n "$RELEASE" && "$OCP_PAYLOAD" != *"$RELEASE"* ]]; then
    echo "Error: OCP_PAYLOAD must contain the RELEASE version. OCP_PAYLOAD='$OCP_PAYLOAD' does not contain RELEASE='$RELEASE'."
    exit 1
fi

prefix="periodic-ci-openshift-cluster-authentication-operator-release"
jobs=(
  ${prefix}-${RELEASE}-periodics-e2e-aws-external-oidc-configure-techpreview
  ${prefix}-${RELEASE}-periodics-e2e-aws-external-oidc-revertoauth-techpreview
  ${prefix}-${RELEASE}-periodics-e2e-aws-external-oidc-uid-extra-techpreview
  ${prefix}-${RELEASE}-periodics-e2e-aws-external-oidc-upstream-parity-techpreview
  ${prefix}-${RELEASE}-periodics-e2e-aws-sno-external-oidc-configure-techpreview
  ${prefix}-${RELEASE}-periodics-e2e-aws-sno-external-oidc-revertoauth-techpreview
  ${prefix}-${RELEASE}-periodics-e2e-aws-sno-external-oidc-uid-extra-techpreview
  ${prefix}-${RELEASE}-periodics-e2e-aws-sno-external-oidc-upstream-parity-techpreview
  ${prefix}-${RELEASE}-periodics-e2e-azure-external-oidc-configure-techpreview
  ${prefix}-${RELEASE}-periodics-e2e-azure-external-oidc-revertoauth-techpreview
  ${prefix}-${RELEASE}-periodics-e2e-azure-external-oidc-uid-extra-techpreview
  ${prefix}-${RELEASE}-periodics-e2e-azure-external-oidc-upstream-parity-techpreview
  ${prefix}-${RELEASE}-periodics-e2e-gcp-external-oidc-configure-techpreview
  ${prefix}-${RELEASE}-periodics-e2e-gcp-external-oidc-revertoauth-techpreview
  ${prefix}-${RELEASE}-periodics-e2e-gcp-external-oidc-uid-extra-techpreview
  ${prefix}-${RELEASE}-periodics-e2e-gcp-external-oidc-upstream-parity-techpreview
  ${prefix}-${RELEASE}-periodics-e2e-metal-dualstack-external-oidc-upstream-parity-techpreview
  ${prefix}-${RELEASE}-periodics-e2e-metal-ovn-dualstack-external-oidc-configure-techpreview
  ${prefix}-${RELEASE}-periodics-e2e-metal-ovn-dualstack-external-oidc-revertoauth-techpreview
  ${prefix}-${RELEASE}-periodics-e2e-metal-ovn-dualstack-external-oidc-uid-extra-techpreview
  ${prefix}-${RELEASE}-periodics-e2e-metal-ovn-ipv4-external-oidc-configure-techpreview
  ${prefix}-${RELEASE}-periodics-e2e-metal-ovn-ipv4-external-oidc-revertoauth-techpreview
  ${prefix}-${RELEASE}-periodics-e2e-metal-ovn-ipv4-external-oidc-uid-extra-techpreview
  ${prefix}-${RELEASE}-periodics-e2e-metal-ovn-ipv4-external-oidc-upstream-parity-techpreview
  ${prefix}-${RELEASE}-periodics-e2e-metal-ovn-ipv6-external-oidc-configure-techpreview
  ${prefix}-${RELEASE}-periodics-e2e-metal-ovn-ipv6-external-oidc-revertoauth-techpreview
  ${prefix}-${RELEASE}-periodics-e2e-metal-ovn-ipv6-external-oidc-uid-extra-techpreview
  ${prefix}-${RELEASE}-periodics-e2e-metal-ovn-ipv6-external-oidc-upstream-parity-techpreview
  ${prefix}-${RELEASE}-periodics-e2e-vsphere-external-oidc-configure-techpreview
  ${prefix}-${RELEASE}-periodics-e2e-vsphere-external-oidc-revertoauth-techpreview
  ${prefix}-${RELEASE}-periodics-e2e-vsphere-external-oidc-uid-extra-techpreview
  ${prefix}-${RELEASE}-periodics-e2e-vsphere-external-oidc-upstream-parity-techpreview
)

num_runs=${NUM_JOBS:-5}

selected_jobs=()
for job in "${jobs[@]}"; do
  if [[ -n "$JOB_FILTER" && "$job" != *"$JOB_FILTER"* ]]; then
    echo "Skipping job '${job}' (does not match filter: $JOB_FILTER)"
    continue
  fi
  selected_jobs+=("$job")
done

if [[ ${#selected_jobs[@]} -eq 0 ]]; then
  echo "No jobs match JOB_FILTER='${JOB_FILTER}'. No jobs were started." >&2
  exit 1
fi

echo "Selected ${#selected_jobs[@]} jobs, ${num_runs} runs each, using payload '${OCP_PAYLOAD}':"
printf '  %s\n' "${selected_jobs[@]}"
if ! read -r -p "Start these jobs? [y/N] " confirmation || [[ "$confirmation" != [yY] ]]; then
  echo "Cancelled. No jobs were started."
  exit 1
fi

for job in "${selected_jobs[@]}"; do
  echo "Running job '${job}' ${num_runs} times"
  echo "****************************"
  gangway-cli \
    --api-url="https://gangway-ci.apps.ci.l2s4.p1.openshiftapps.com" \
    --initial="registry.ci.openshift.org/ocp/release:${OCP_PAYLOAD}" \
    --latest="registry.ci.openshift.org/ocp/release:${OCP_PAYLOAD}" \
    --job-name="${job}" \
    --num-jobs=${num_runs} \
    --jobs-file-path="runs/"
  echo "****************************"
  sleep 30
done
