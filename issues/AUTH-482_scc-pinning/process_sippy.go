package main

import (
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
)

type SippyTest struct {
	Name             string `json:"name"`
	JiraComponent    string `json:"jira_component"`
	CurrentRuns      int    `json:"current_runs"`
	CurrentSuccesses int    `json:"current_successes"`
	CurrentFlakes    int    `json:"current_flakes"`
	CurrentFailures  int    `json:"current_failures"`

	Namespace string
}

type nsProgress struct {
	prs         []string
	done        bool
	runlevel    string
	noFixNeeded bool
}

var out io.Writer

func main() {
	if len(os.Args) < 2 {
		fmt.Println("no sippy file provided")
		os.Exit(1)
	}

	sippyFile := os.Args[1]
	data, err := os.ReadFile(sippyFile)
	if err != nil {
		panic(err)
	}

	out = os.Stdout
	if len(os.Args) >= 3 {
		out, err = os.Create(os.Args[2])
		if err != nil {
			panic(err)
		}
	}

	fmt.Println("checking status of namespaces")
	for nsName, ns := range progressPerNs {
		if ns.done {
			continue
		}

		allMerged := true
		for _, pr := range ns.prs {
			status := prStatus(pr)
			if status == "OPEN" {
				allMerged = false
				break
			}
		}

		if len(ns.prs) > 0 && allMerged {
			fmt.Printf("* all PRs of %s have been closed\n", nsName)
		}
	}

	fmt.Println("\nreading sippy tests")
	var tests []*SippyTest
	if err := json.Unmarshal(data, &tests); err != nil {
		panic(err)
	}
	fmt.Printf("found %d tests\n", len(tests))

	slices.SortStableFunc(tests, func(a, b *SippyTest) int {
		return cmp.Compare(a.CurrentFlakes, b.CurrentFlakes)
	})

	runlevel := make([]*SippyTest, 0)
	nonRunlevel := make([]*SippyTest, 0)
	unknownRunlevel := make([]*SippyTest, 0)
	for _, t := range tests {
		t.Namespace = getNamespace(t.Name)
		fmt.Println("*", t.Namespace)

		ns := progressPerNs[t.Namespace]

		if ns.noFixNeeded && t.CurrentFlakes > 0 {
			fmt.Println("  > no fix needed but is flaking")
		}

		if ns.runlevel == "unknown" {
			ns.runlevel = getRunlevel(t.Namespace)
			fmt.Println("  > runlevel:", ns.runlevel)
		}

		switch ns.runlevel {
		case "unknown":
			unknownRunlevel = append(unknownRunlevel, t)
		case "yes":
			runlevel = append(runlevel, t)
		case "no", "":
			nonRunlevel = append(nonRunlevel, t)
		}
	}

	header := "|  #  | Component | Namespace | # Runs | # Successes | # Flakes | # Failures | Status | PRs |"
	subhdr := "| --- | --------- | --------- | ------ | ----------- | -------- | ---------- | ------ | --- |"

	if len(os.Args) > 2 {
		fmt.Printf("writing results to: %s\n", os.Args[2])
	}

	fmt.Fprintf(out, "%s\n", stats())
	fmt.Fprintln(out, "## Non-runlevel")
	fmt.Fprintln(out, header)
	fmt.Fprintln(out, subhdr)
	for i, t := range nonRunlevel {
		print(i, t)
	}

	fmt.Fprintln(out, "\n## Runlevel")
	fmt.Fprintln(out, header)
	fmt.Fprintln(out, subhdr)
	for i, t := range runlevel {
		print(i, t)
	}

	fmt.Fprintln(out, "\n## Unknown runlevel")
	fmt.Fprintln(out, header)
	fmt.Fprintln(out, subhdr)
	for i, t := range unknownRunlevel {
		print(i, t)
	}

	fmt.Fprintln(out, "\n## Jira blob")
	fmt.Fprintf(out, "```\n%s```", jiraBlob())
}

func prStatus(url string) string {
	// gh pr view 1038 --repo openshift/cluster-version-operator --json state -q '.state'
	// https://github.com/openshift/cluster-openshift-apiserver-operator/pull/573
	matches := regexp.MustCompile(`https\:\/\/github\.com\/(.*)\/pull\/(\d+)`).FindStringSubmatch(url)
	if len(matches) != 3 {
		panic("bad PR url format: " + url)
	}

	var out, stderr strings.Builder
	cmd := exec.Command("gh", "pr", "view", matches[2], "--repo", matches[1], "--json", "state", "-q", ".state")
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		panic(fmt.Errorf("cmd '%s' failed with error: %s", cmd.String(), stderr.String()))
	}

	return strings.TrimSpace(out.String())
}

func getNamespace(testName string) string {
	ns := testName
	ns = strings.ReplaceAll(ns, "[sig-auth] all workloads in ns/", "")
	ns = strings.ReplaceAll(ns, " must set the 'openshift.io/required-scc' annotation", "")
	return ns
}

func getRunlevel(ns string) string {
	var out strings.Builder
	cmd := exec.Command("oc", "get", "ns", ns, "-o", "jsonpath='{.metadata.labels.openshift\\.io/run-level}'")
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "unknown"
	}

	if len(strings.ReplaceAll(out.String(), "'", "")) > 0 {
		return "yes"
	}

	return "no"
}

func stats() string {
	cntDone := 0
	numPRs := 0
	numRunlevel := 0
	numRunlevelNotFixed := 0
	numNoFixNeeded := 0
	for _, ns := range progressPerNs {
		if ns.done {
			cntDone++
		}

		if ns.runlevel == "yes" {
			numRunlevel++
			if !ns.done {
				numRunlevelNotFixed++
			}
		}

		if ns.noFixNeeded {
			numNoFixNeeded++
		}

		numPRs += len(ns.prs)
	}

	totalNS := len(progressPerNs)
	remaining := totalNS - cntDone - numNoFixNeeded - (numRunlevel - numRunlevelNotFixed)
	return fmt.Sprintf("%d namespaces, %d PRs\n- fixed: %d\n- already OK: %d\n- total remaining: %d (%d non-runlevel, %d runlevel)\n",
		totalNS,
		numPRs,
		cntDone,
		numNoFixNeeded,
		remaining,
		remaining-numRunlevelNotFixed,
		numRunlevelNotFixed,
	)
}

func print(i int, t *SippyTest) {
	nsprog := progressPerNs[t.Namespace]
	prs := []string{}
	for _, pr := range progressPerNs[t.Namespace].prs {
		prs = append(prs, fmt.Sprintf("[%s](%s)", prName(pr), pr))
	}

	var status string
	if nsprog.done {
		status = "done"
	} else if nsprog.noFixNeeded {
		status = "ok"
	}

	fmt.Fprintf(out, "| %d | %s | %s | %d | %d | %d | %d | %s | %s |\n",
		i+1,
		t.JiraComponent,
		t.Namespace,
		t.CurrentRuns,
		t.CurrentSuccesses,
		t.CurrentFlakes,
		t.CurrentFailures,
		status,
		strings.Join(prs, ", "),
	)
}

func jiraBlob() string {
	nses := make([]string, 0, len(progressPerNs))
	for ns := range progressPerNs {
		nses = append(nses, ns)
	}

	slices.Sort(nses)

	var jiraBlob bytes.Buffer
	for i, ns := range nses {
		prs := make([]string, 0)
		for _, pr := range progressPerNs[ns].prs {
			prs = append(prs, fmt.Sprintf("[%s|%s]", prName(pr), pr))
		}

		statusStr := ""
		if progressPerNs[ns].done {
			statusStr = "(/)"
		}

		jiraBlob.WriteString(fmt.Sprintf("| %d | %s | %s | %s |\n",
			i+1,
			ns,
			strings.Join(prs, ", "),
			statusStr,
		))
	}

	return jiraBlob.String()
}

func prName(url string) string {
	parts := strings.Split(url, "/")
	return fmt.Sprintf("#%s", parts[len(parts)-1])
}

var progressPerNs = map[string]*nsProgress{"openshift-controller-manager-operator": {
	done: true,
	prs: []string{
		"https://github.com/openshift/cluster-openshift-controller-manager-operator/pull/336",
	},
},
	"openshift-etcd-operator": {
		runlevel: "yes",
		done:     false,
		prs:      []string{},
	},
	"openshift-insights": {
		done: true,
		prs: []string{
			"https://github.com/openshift/insights-operator/pull/915",
		},
	},
	"openshift-kube-controller-manager-operator": {
		runlevel: "yes",
		done:     false,
		prs:      []string{},
	},
	"openshift-ovn-kubernetes": {
		runlevel: "yes",
		done:     false,
		prs:      []string{},
	},
	"openshift-console": {
		done: false,
		prs: []string{
			"https://github.com/openshift/console-operator/pull/871",
		},
	},
	"openshift-controller-manager": {
		done: true,
		prs: []string{
			"https://github.com/openshift/cluster-openshift-controller-manager-operator/pull/336",
		},
	},
	"openshift-monitoring": {
		done: true,
		prs: []string{
			"https://github.com/openshift/cluster-monitoring-operator/pull/2335",
		},
	},
	"openshift-route-controller-manager": {
		done: true,
		prs: []string{
			"https://github.com/openshift/cluster-openshift-controller-manager-operator/pull/336",
		},
	},
	"openshift-cluster-olm-operator": {
		runlevel: "unknown",
		done:     true,
		prs: []string{
			"https://github.com/openshift/cluster-olm-operator/pull/54",
		},
	},
	"openshift-ingress": {
		done: false,
		prs: []string{
			"https://github.com/openshift/cluster-ingress-operator/pull/1031",
		},
	},
	"openshift-kube-apiserver": {
		runlevel: "yes",
		done:     false,
		prs:      []string{},
	},
	"openshift-marketplace": {
		done: true,
		prs: []string{
			"https://github.com/operator-framework/operator-marketplace/pull/561",
		},
	},
	"openshift-network-node-identity": {
		done: false,
		prs: []string{
			"https://github.com/openshift/cluster-network-operator/pull/2282",
		},
	},
	"openshift-operator-lifecycle-manager": {
		done: true,
		prs: []string{
			"https://github.com/openshift/operator-framework-olm/pull/703",
		},
	},
	"openshift-user-workload-monitoring": {
		done: true,
		prs: []string{
			"https://github.com/openshift/cluster-monitoring-operator/pull/2335",
		},
	},
	"openshift-config-operator": {
		done: true,
		prs: []string{
			"https://github.com/openshift/cluster-config-operator/pull/410",
		},
	},
	"openshift-kube-storage-version-migrator": {
		done: true,
		prs: []string{
			"https://github.com/openshift/cluster-kube-storage-version-migrator-operator/pull/107",
		},
	},
	"openshift-cloud-credential-operator": {
		done: true,
		prs: []string{
			"https://github.com/openshift/cloud-credential-operator/pull/681",
		},
	},
	"openshift-cluster-storage-operator": {
		done: true,
		prs: []string{
			"https://github.com/openshift/cluster-storage-operator/pull/459",
			"https://github.com/openshift/cluster-csi-snapshot-controller-operator/pull/196",
		},
	},
	"openshift-machine-config-operator": {
		done: true,
		prs: []string{
			"https://github.com/openshift/machine-config-operator/pull/4219",
		},
	},
	"openshift-cloud-controller-manager": {
		runlevel: "yes",
		done:     false,
		prs:      []string{},
	},
	"openshift-dns-operator": {
		runlevel: "yes",
		done:     false,
		prs:      []string{},
	},
	"openshift-network-diagnostics": {
		done: false,
		prs: []string{
			"https://github.com/openshift/cluster-network-operator/pull/2282",
		},
	},
	"openshift-cluster-machine-approver": {
		runlevel: "yes",
		done:     false,
		prs:      []string{},
	},
	"openshift-catalogd": {
		runlevel: "unknown",
		done:     true,
		prs: []string{
			"https://github.com/openshift/operator-framework-catalogd/pull/50",
		},
	},
	"openshift-cloud-controller-manager-operator": {
		runlevel: "yes",
		done:     false,
		prs:      []string{},
	},
	"openshift-cluster-node-tuning-operator": {
		done: true,
		prs: []string{
			"https://github.com/openshift/cluster-node-tuning-operator/pull/968",
		},
	},
	"openshift-image-registry": {
		done: true,
		prs: []string{
			"https://github.com/openshift/cluster-image-registry-operator/pull/1008",
		},
	},
	"openshift-ingress-canary": {
		done: false,
		prs: []string{
			"https://github.com/openshift/cluster-ingress-operator/pull/1031",
		},
	},
	"openshift-ingress-operator": {
		done: false,
		prs: []string{
			"https://github.com/openshift/cluster-ingress-operator/pull/1031",
		},
	},
	"openshift-kube-apiserver-operator": {
		runlevel: "yes",
		done:     false,
		prs:      []string{},
	},
	"openshift-authentication": {
		done: true,
		prs: []string{
			"https://github.com/openshift/cluster-authentication-operator/pull/656",
		},
	},
	"openshift-service-ca-operator": {
		done: true,
		prs: []string{
			"https://github.com/openshift/service-ca-operator/pull/235",
		},
	},
	"openshift-oauth-apiserver": {
		done: true,
		prs: []string{
			"https://github.com/openshift/cluster-authentication-operator/pull/656",
		},
	},
	"openshift-cloud-network-config-controller": {
		done: false,
		prs: []string{
			"https://github.com/openshift/cluster-network-operator/pull/2282",
		},
	},
	"openshift-cluster-samples-operator": {
		done: true,
		prs: []string{
			"https://github.com/openshift/cluster-samples-operator/pull/535",
		},
	},
	"openshift-kube-storage-version-migrator-operator": {
		done: true,
		prs: []string{
			"https://github.com/openshift/cluster-kube-storage-version-migrator-operator/pull/107",
		},
	},
	"openshift-operator-controller": {
		runlevel: "unknown",
		done:     true,
		prs: []string{
			"https://github.com/openshift/operator-framework-operator-controller/pull/100",
		},
	},
	"openshift-service-ca": {
		done: true,
		prs: []string{
			"https://github.com/openshift/service-ca-operator/pull/235",
		},
	},
	"openshift-etcd": {
		runlevel: "yes",
		done:     false,
		prs:      []string{},
	},
	"openshift-dns": {
		runlevel: "yes",
		done:     false,
		prs:      []string{},
	},
	"openshift-cluster-csi-drivers": {
		done: true,
		prs: []string{
			"https://github.com/openshift/csi-operator/pull/170",
			"https://github.com/openshift/cluster-storage-operator/pull/459",
		},
	},
	"openshift-console-operator": {
		done: false,
		prs: []string{
			"https://github.com/openshift/console-operator/pull/871",
		},
	},
	"openshift-authentication-operator": {
		done: true,
		prs: []string{
			"https://github.com/openshift/cluster-authentication-operator/pull/656",
		},
	},
	"openshift-machine-api": {
		done: true,
		prs: []string{
			"https://github.com/openshift/cluster-autoscaler-operator/pull/315",
			"https://github.com/openshift/cluster-baremetal-operator/pull/407",
			"https://github.com/openshift/cluster-control-plane-machine-set-operator/pull/282",
			"https://github.com/openshift/machine-api-operator/pull/1220",
			"https://github.com/openshift/machine-api-provider-nutanix/pull/73",
			"https://github.com/openshift/cluster-api-provider-alibaba/pull/50",
		},
	},
	"openshift-multus": {
		runlevel: "yes",
		done:     false,
		prs:      []string{},
	},
	"openshift-network-operator": {
		runlevel: "yes",
		done:     false,
		prs:      []string{},
	},
	"openshift-apiserver-operator": {
		done: true,
		prs: []string{
			"https://github.com/openshift/cluster-openshift-apiserver-operator/pull/573",
		},
	},
	"openshift-kube-scheduler": {
		runlevel: "yes",
		done:     false,
		prs:      []string{},
	},
	"openshift-cluster-version": {
		done: true,
		prs: []string{
			"https://github.com/openshift/cluster-version-operator/pull/1038",
		},
	},
	"openshift-kube-scheduler-operator": {
		runlevel: "yes",
		done:     false,
		prs:      []string{},
	},
	"openshift-kube-controller-manager": {
		runlevel: "yes",
		done:     false,
		prs:      []string{},
	},
	"openshift-platform-operators": {
		runlevel: "unknown",
		done:     false,
		prs:      []string{},
	},
	"openshift-e2e-loki": {
		runlevel: "unknown",
		done:     false,
		prs:      []string{},
	},
	"openshift-kni-infra": {
		done: false,
		prs:  []string{},
	},
	"openshift-vsphere-infra": {
		done: false,
		prs:  []string{},
	},
	"openshift-ovirt-infra": {
		noFixNeeded: true,
	},
	"openshift-openstack-infra": {
		done: false,
		prs:  []string{},
	},
	"openshift-nutanix-infra": {
		done: false,
		prs:  []string{},
	},
	"openshift-cloud-platform-infra": {
		noFixNeeded: true,
	},
	"openshift-apiserver": {
		noFixNeeded: true,
	},
	"openshift-rukpak": {
		runlevel: "unknown",
		done:     false,
		prs:      []string{},
	},
	"openshift-metallb-system": {
		runlevel: "unknown",
		done:     false,
		prs:      []string{},
	},
	"openshift-manila-csi-driver": {
		runlevel: "unknown",
		done:     false,
		prs:      []string{},
	},
	"openshift-kube-proxy": {
		runlevel: "unknown",
		done:     false,
		prs:      []string{},
	},
	"openshift-sriov-network-operator": {
		runlevel: "unknown",
		done:     false,
		prs:      []string{},
	},
	"openshift-cluster-api": {
		runlevel: "unknown",
		done:     false,
		prs:      []string{},
	},
	"openshift-sdn": {
		runlevel: "unknown",
		done:     false,
		prs:      []string{},
	},
	"openshift-host-network": {
		noFixNeeded: true,
	},
	"openshift-operators": {
		noFixNeeded: true,
	},
	"openshift-console-user-settings": {
		noFixNeeded: true,
	},
	"openshift-config": {
		runlevel: "yes",
		done:     false,
		prs:      []string{},
	},
	"openshift-config-managed": {
		runlevel: "yes",
		done:     false,
		prs:      []string{},
	},
	"openshift-infra": {
		noFixNeeded: true,
	},
	"openshift": {
		noFixNeeded: true,
	},
	"openshift-node": {
		noFixNeeded: true,
	},
	"default": {
		done: false,
		prs:  []string{},
	},
	"kube-public": {
		noFixNeeded: true,
	},
	"kube-node-lease": {
		noFixNeeded: true,
	},
	"kube-system": {
		done: false,
		prs:  []string{},
	},
	"oc debug node pods": {
		done: true,
		prs:  []string{"https://github.com/openshift/oc/pull/1763"},
	},
}
