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

if [[ -z "$JOB_VARIANT" ]]; then
    echo "No job variant set. Please select a job variant from [configure, rollback, uid-extra, all] and set JOB_VARIANT to the one you've selected."
    exit 1
fi

if [[ -z "$RELEASE" ]]; then
    echo "No release set. Please select a release from [4.20, 4.21] and set RELEASE to the one you've selected."
    exit 1
fi

if [[ -n "$RELEASE" && "$OCP_PAYLOAD" != *"$RELEASE"* ]]; then
    echo "Error: OCP_PAYLOAD must contain the RELEASE version. OCP_PAYLOAD='$OCP_PAYLOAD' does not contain RELEASE='$RELEASE'."
    exit 1
fi


configure_jobs=(
    "periodic-ci-openshift-cluster-authentication-operator-release-${RELEASE}-periodics-e2e-vsphere-external-oidc-configure-techpreview"
    "periodic-ci-openshift-cluster-authentication-operator-release-${RELEASE}-periodics-e2e-azure-external-oidc-configure-techpreview"
    "periodic-ci-openshift-cluster-authentication-operator-release-${RELEASE}-periodics-e2e-gcp-external-oidc-configure-techpreview"
    "periodic-ci-openshift-cluster-authentication-operator-release-${RELEASE}-periodics-e2e-aws-external-oidc-configure-techpreview"
    "periodic-ci-openshift-cluster-authentication-operator-release-${RELEASE}-periodics-e2e-metal-ovn-ipv4-external-oidc-configure-techpreview"
    "periodic-ci-openshift-cluster-authentication-operator-release-${RELEASE}-periodics-e2e-metal-ovn-dualstack-external-oidc-configure-techpreview"
    "periodic-ci-openshift-cluster-authentication-operator-release-${RELEASE}-periodics-e2e-aws-sno-external-oidc-configure-techpreview"
)

rollback_jobs=(
    "periodic-ci-openshift-cluster-authentication-operator-release-${RELEASE}-periodics-e2e-vsphere-external-oidc-rollback-techpreview"
    "periodic-ci-openshift-cluster-authentication-operator-release-${RELEASE}-periodics-e2e-azure-external-oidc-rollback-techpreview"
    "periodic-ci-openshift-cluster-authentication-operator-release-${RELEASE}-periodics-e2e-gcp-external-oidc-rollback-techpreview"
    "periodic-ci-openshift-cluster-authentication-operator-release-${RELEASE}-periodics-e2e-aws-external-oidc-rollback-techpreview"
    "periodic-ci-openshift-cluster-authentication-operator-release-${RELEASE}-periodics-e2e-metal-ovn-ipv4-external-oidc-rollback-techpreview"
    "periodic-ci-openshift-cluster-authentication-operator-release-${RELEASE}-periodics-e2e-metal-ovn-dualstack-external-oidc-rollback-techpreview"
    "periodic-ci-openshift-cluster-authentication-operator-release-${RELEASE}-periodics-e2e-aws-sno-external-oidc-rollback-techpreview"
)

uid_extra_jobs=(
    "periodic-ci-openshift-cluster-authentication-operator-release-${RELEASE}-periodics-e2e-vsphere-external-oidc-uid-extra-techpreview"
    "periodic-ci-openshift-cluster-authentication-operator-release-${RELEASE}-periodics-e2e-gcp-external-oidc-uid-extra-techpreview"
    "periodic-ci-openshift-cluster-authentication-operator-release-${RELEASE}-periodics-e2e-azure-external-oidc-uid-extra-techpreview"
    "periodic-ci-openshift-cluster-authentication-operator-release-${RELEASE}-periodics-e2e-aws-external-oidc-uid-extra-techpreview"
    "periodic-ci-openshift-cluster-authentication-operator-release-${RELEASE}-periodics-e2e-metal-ovn-ipv4-external-oidc-uid-extra-techpreview"
    "periodic-ci-openshift-cluster-authentication-operator-release-${RELEASE}-periodics-e2e-metal-ovn-dualstack-external-oidc-uid-extra-techpreview"
    "periodic-ci-openshift-cluster-authentication-operator-release-${RELEASE}-periodics-e2e-aws-sno-external-oidc-uid-extra-techpreview"
)

num_runs=5

if [[ "$JOB_VARIANT" == "configure" || "$JOB_VARIANT" == "all" ]]; then
    for job in "${configure_jobs[@]}"; do
        if [[ -n "$JOB_FILTER" && "$job" != *"$JOB_FILTER"* ]]; then
            echo "Skipping job '${job}' (does not match filter: $JOB_FILTER)"
            continue
        fi
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
        sleep 60
    done
fi

if [[ "$JOB_VARIANT" == "rollback" || "$JOB_VARIANT" == "all" ]]; then
    for job in "${rollback_jobs[@]}"; do
        if [[ -n "$JOB_FILTER" && "$job" != *"$JOB_FILTER"* ]]; then
            echo "Skipping job '${job}' (does not match filter: $JOB_FILTER)"
            continue
        fi
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
        sleep 60
    done
fi

if [[ "$JOB_VARIANT" == "uid-extra" || "$JOB_VARIANT" == "all" ]]; then
    for job in "${uid_extra_jobs[@]}"; do
        if [[ -n "$JOB_FILTER" && "$job" != *"$JOB_FILTER"* ]]; then
            echo "Skipping job '${job}' (does not match filter: $JOB_FILTER)"
            continue
        fi
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
        sleep 60
    done
fi
