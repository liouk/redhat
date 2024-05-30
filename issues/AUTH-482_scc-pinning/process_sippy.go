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
	"time"
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

type versionProgress struct {
	done bool
	prs  []string
}

type nsProgress struct {
	prsPerVersion map[string]versionProgress
	runlevel      string
	noFixNeeded   bool
}

func (ns *nsProgress) prsForVersion(version string) []string {
	if ns.prsPerVersion == nil {
		return nil
	}

	return ns.prsPerVersion[version].prs
}

type stats struct {
	numPRs                    int
	numOpenPRs                int
	numNS                     int
	numDoneNS                 int
	numNoFixNeededNS          int
	numRemainingRunlevelNS    int
	numRemainingNonRunlevelNS int
}

const (
	v415 = "4.15"
	v416 = "4.16"
	v417 = "4.17"
)

var (
	out io.Writer

	versions = []string{v415, v416, v417}

	uniquePRs = map[string]struct{}{}

	versionStats = map[string]*stats{
		v415: {},
		v416: {},
		v417: {},
	}

	untestedNS = map[string]struct{}{}
)

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

	for _, v := range versions {
		vstats := versionStats[v]
		vstats.numNS += len(progressPerNs)
	}

	fmt.Println("checking status of namespaces")
	for nsName, ns := range progressPerNs {
		prevDone := false
		for _, v := range versions {
			vstats := versionStats[v]
			vstats.numPRs += len(ns.prsPerVersion[v].prs)
			for _, pr := range ns.prsPerVersion[v].prs {
				uniquePRs[pr] = struct{}{}
			}

			if ns.noFixNeeded {
				vstats.numNoFixNeededNS++
				continue
			} else if ns.prsPerVersion[v].done || prevDone {
				vstats.numDoneNS++
				prevDone = true
				continue
			}

			if ns.runlevel == "yes" {
				vstats.numRemainingRunlevelNS++
			} else {
				vstats.numRemainingNonRunlevelNS++
			}

			allMerged := true
			for _, pr := range ns.prsPerVersion[v].prs {
				if prStatus(pr) == "OPEN" {
					vstats.numOpenPRs += 1
					allMerged = false
					break
				}
			}

			if len(ns.prsPerVersion[v].prs) > 0 && allMerged {
				fmt.Printf("* all v%s PRs of %s have been closed\n", v, nsName)
			}
		}
	}

	for _, v := range versions {
		vstats := versionStats[v]
		vstats.numPRs = len(uniquePRs)
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

	for ns, _ := range progressPerNs {
		untestedNS[ns] = struct{}{}
	}

	runlevel := make([]*SippyTest, 0)
	nonRunlevel := make([]*SippyTest, 0)
	unknownRunlevel := make([]*SippyTest, 0)
	for _, t := range tests {
		t.Namespace = getNamespace(t.Name)
		delete(untestedNS, t.Namespace)

		ns := progressPerNs[t.Namespace]

		if ns.noFixNeeded && t.CurrentFlakes > 0 {
			fmt.Printf("* %s: no fix needed but is flaking\n", t.Namespace)
		}

		if ns.runlevel == "unknown" {
			ns.runlevel = getRunlevel(t.Namespace)
			fmt.Printf("* %s runlevel for ns %s\n", ns.runlevel, t.Namespace)
		}

		switch ns.runlevel {
		case "unknown":
			unknownRunlevel = append(unknownRunlevel, t)
		case "yes":
			runlevel = append(runlevel, t)
		case "no", "":
			nonRunlevel = append(nonRunlevel, t)
		default:
			panic(fmt.Sprintf("unexpected runlevel string: %s", ns.runlevel))
		}
	}

	header := "|  #  | Component | Namespace | # Runs | # Successes | # Flakes | # Failures | 4.17 | 4.16 | 4.15 |"
	subhdr := "| --- | --------- | --------- | ------ | ----------- | -------- | ---------- | ---- | ---- | ---- |"

	fmt.Fprintf(out, "*Last updated: %s*\n\n", time.Now().Format(time.DateTime))
	fmt.Fprintf(out, "%s\n", getStats())
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

	fmt.Fprintln(out, "\n## Untested NS")
	fmt.Fprintln(out, header)
	fmt.Fprintln(out, subhdr)
	i := 1
	for ns := range untestedNS {
		print(i, &SippyTest{Namespace: ns})
		i++
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

func getStats() string {

	var statsBuf bytes.Buffer
	statsBuf.WriteString("[Open PRs](https://github.com/pulls?q=is%3Apr+author%3Aliouk+archived%3Afalse+AUTH-482+in%3Atitle+is%3Aopen)\n\n[Jira issue](https://issues.redhat.com/browse/AUTH-482)\n\n")
	statsBuf.WriteString("| Version | 4.17 | 4.16 | 4.15 |\n")
	statsBuf.WriteString("| ------- | ---- | ---- | ---- |\n")

	statsBuf.WriteString(fmt.Sprintf("| open PRs | %d/%d | %d/%d | %d/%d |\n",
		versionStats[v417].numOpenPRs, versionStats[v417].numPRs,
		versionStats[v416].numOpenPRs, versionStats[v416].numPRs,
		versionStats[v415].numOpenPRs, versionStats[v415].numPRs,
	))
	statsBuf.WriteString(fmt.Sprintf("| num NS | %d | %d | %d |\n",
		versionStats[v417].numNS, versionStats[v416].numNS, versionStats[v415].numNS,
	))
	statsBuf.WriteString(fmt.Sprintf("| untested NS | %d | %d | %d |\n",
		len(untestedNS), len(untestedNS), len(untestedNS),
	))
	statsBuf.WriteString(fmt.Sprintf("| ready NS | %d | %d | %d |\n",
		versionStats[v417].numDoneNS+versionStats[v417].numNoFixNeededNS, versionStats[v416].numDoneNS+versionStats[v416].numNoFixNeededNS, versionStats[v415].numDoneNS+versionStats[v415].numNoFixNeededNS,
	))
	// untested namespaces end up being counted as remaining-non-runlevel
	statsBuf.WriteString(fmt.Sprintf("| remaining non-runlevel NS | %d | %d | %d |\n",
		versionStats[v417].numRemainingNonRunlevelNS-len(untestedNS), versionStats[v416].numRemainingNonRunlevelNS-len(untestedNS), versionStats[v415].numRemainingNonRunlevelNS-len(untestedNS),
	))
	statsBuf.WriteString(fmt.Sprintf("| remaining runlevel NS | %d | %d | %d |\n",
		versionStats[v417].numRemainingRunlevelNS, versionStats[v416].numRemainingRunlevelNS, versionStats[v415].numRemainingRunlevelNS,
	))

	return statsBuf.String()
}

func print(i int, t *SippyTest) {
	nsprog := progressPerNs[t.Namespace]

	prLine := map[string]string{v417: "", v416: "", v415: ""}
	prevDone := false
	for _, v := range versions {
		status := ""
		if nsprog.noFixNeeded {
			status = "ready"
		} else if nsprog.prsPerVersion[v].done {
			status = "DONE; "
		} else if prevDone {
			status = "n/a"
		}

		prs := make([]string, 0)
		for _, pr := range nsprog.prsPerVersion[v].prs {
			prs = append(prs, fmt.Sprintf("[%s](%s)", prName(pr), pr))
		}

		prLine[v] = fmt.Sprintf("%s%s", status, strings.Join(prs, " "))
		prevDone = nsprog.prsPerVersion[v].done
	}

	fmt.Fprintf(out, "| %d | %s | %s | %d | %d | %d | %d | %s | %s | %s |\n",
		i+1,
		t.JiraComponent,
		t.Namespace,
		t.CurrentRuns,
		t.CurrentSuccesses,
		t.CurrentFlakes,
		t.CurrentFailures,
		prLine[v417],
		prLine[v416],
		prLine[v415],
	)
}

func jiraBlob() string {
	nses := make([]string, 0, len(progressPerNs))
	for ns := range progressPerNs {
		nses = append(nses, ns)
	}

	slices.Sort(nses)

	var jiraBlob bytes.Buffer
	jiraBlob.WriteString("||#||namespace||4.17||4.16||4.15||\n")

	for i, ns := range nses {
		prLine := map[string]string{
			v417: "",
			v416: "",
			v415: "",
		}

		for _, v := range versions {
			status := ""
			if progressPerNs[ns].prsPerVersion[v].done {
				status = "(/) "
			}

			prs := make([]string, 0)
			for _, pr := range progressPerNs[ns].prsPerVersion[v].prs {
				prs = append(prs, fmt.Sprintf("[%s|%s]", prName(pr), pr))
			}
			prLine[v] = fmt.Sprintf("%s%s", status, strings.Join(prs, " "))
		}

		jiraBlob.WriteString(fmt.Sprintf("| %d | %s | %s | %s | %s |\n",
			i+1,
			ns,
			prLine[v417],
			prLine[v416],
			prLine[v415],
		))
	}

	return jiraBlob.String()
}

func prName(url string) string {
	parts := strings.Split(url, "/")
	return fmt.Sprintf("#%s", parts[len(parts)-1])
}

var progressPerNs = map[string]*nsProgress{
	"openshift-controller-manager-operator": {
		prsPerVersion: map[string]versionProgress{
			v416: {
				done: true,
				prs:  []string{"https://github.com/openshift/cluster-openshift-controller-manager-operator/pull/336"},
			},
		},
	},
	"openshift-etcd-operator": {
		runlevel: "yes",
	},
	"openshift-insights": {
		prsPerVersion: map[string]versionProgress{
			v416: {
				done: true,
				prs:  []string{"https://github.com/openshift/insights-operator/pull/915"},
			},
		},
	},
	"openshift-kube-controller-manager-operator": {
		runlevel: "yes",
	},
	"openshift-ovn-kubernetes": {
		runlevel: "yes",
	},
	"openshift-console": {
		prsPerVersion: map[string]versionProgress{
			v417: {
				done: true,
				prs:  []string{"https://github.com/openshift/console-operator/pull/871"},
			},
			v416: {
				done: false,
				prs:  []string{"https://github.com/openshift/console-operator/pull/908"},
			},
		},
	},
	"openshift-controller-manager": {
		prsPerVersion: map[string]versionProgress{
			v416: {
				done: true,
				prs:  []string{"https://github.com/openshift/cluster-openshift-controller-manager-operator/pull/336"},
			},
		},
	},
	"openshift-monitoring": {
		prsPerVersion: map[string]versionProgress{
			v416: {
				done: true,
				prs:  []string{"https://github.com/openshift/cluster-monitoring-operator/pull/2335"},
			},
		},
	},
	"openshift-route-controller-manager": {
		prsPerVersion: map[string]versionProgress{
			v416: {
				done: true,
				prs:  []string{"https://github.com/openshift/cluster-openshift-controller-manager-operator/pull/336"},
			},
		},
	},
	"openshift-cluster-olm-operator": {
		runlevel: "unknown",
		prsPerVersion: map[string]versionProgress{
			v416: {
				done: true,
				prs:  []string{"https://github.com/openshift/cluster-olm-operator/pull/54"},
			},
		},
	},
	"openshift-ingress": {
		prsPerVersion: map[string]versionProgress{
			v416: {
				done: false,
				prs:  []string{"https://github.com/openshift/cluster-ingress-operator/pull/1031"},
			},
		},
	},
	"openshift-kube-apiserver": {
		runlevel: "yes",
	},
	"openshift-marketplace": {
		prsPerVersion: map[string]versionProgress{
			v416: {
				done: true,
				prs:  []string{"https://github.com/operator-framework/operator-marketplace/pull/561"},
			},
		},
	},
	"openshift-network-node-identity": {
		prsPerVersion: map[string]versionProgress{
			v417: {
				done: false,
				prs:  []string{"https://github.com/openshift/cluster-network-operator/pull/2282"},
			},
		},
	},
	"openshift-operator-lifecycle-manager": {
		prsPerVersion: map[string]versionProgress{
			v416: {
				done: true,
				prs:  []string{"https://github.com/openshift/operator-framework-olm/pull/703"},
			},
		},
	},
	"openshift-user-workload-monitoring": {
		prsPerVersion: map[string]versionProgress{
			v416: {
				done: true,
				prs:  []string{"https://github.com/openshift/cluster-monitoring-operator/pull/2335"},
			},
		},
	},
	"openshift-config-operator": {
		prsPerVersion: map[string]versionProgress{
			v416: {
				done: true,
				prs:  []string{"https://github.com/openshift/cluster-config-operator/pull/410"},
			},
		},
	},
	"openshift-kube-storage-version-migrator": {
		prsPerVersion: map[string]versionProgress{
			v416: {
				done: true,
				prs:  []string{"https://github.com/openshift/cluster-kube-storage-version-migrator-operator/pull/107"},
			},
		},
	},
	"openshift-cloud-credential-operator": {
		prsPerVersion: map[string]versionProgress{
			v416: {
				done: true,
				prs:  []string{"https://github.com/openshift/cloud-credential-operator/pull/681"},
			},
		},
	},
	"openshift-cluster-storage-operator": {
		prsPerVersion: map[string]versionProgress{
			v416: {
				done: true,
				prs: []string{
					"https://github.com/openshift/cluster-storage-operator/pull/459",
					"https://github.com/openshift/cluster-csi-snapshot-controller-operator/pull/196",
				},
			},
		},
	},
	"openshift-machine-config-operator": {
		prsPerVersion: map[string]versionProgress{
			v417: {
				done: true,
				prs:  []string{"https://github.com/openshift/machine-config-operator/pull/4219"},
			},
		},
	},
	"openshift-cloud-controller-manager": {
		runlevel: "yes",
	},
	"openshift-dns-operator": {
		runlevel: "yes",
	},
	"openshift-network-diagnostics": {
		prsPerVersion: map[string]versionProgress{
			v416: {
				done: false,
				prs:  []string{"https://github.com/openshift/cluster-network-operator/pull/2282"},
			},
		},
	},
	"openshift-cluster-machine-approver": {
		runlevel: "yes",
	},
	"openshift-catalogd": {
		runlevel: "unknown",
		prsPerVersion: map[string]versionProgress{
			v416: {
				done: true,
				prs:  []string{"https://github.com/openshift/operator-framework-catalogd/pull/50"},
			},
		},
	},
	"openshift-cloud-controller-manager-operator": {
		runlevel: "yes",
	},
	"openshift-cluster-node-tuning-operator": {
		prsPerVersion: map[string]versionProgress{
			v416: {
				done: true,
				prs:  []string{"https://github.com/openshift/cluster-node-tuning-operator/pull/968"},
			},
		},
	},
	"openshift-image-registry": {
		prsPerVersion: map[string]versionProgress{
			v416: {
				done: true,
				prs:  []string{"https://github.com/openshift/cluster-image-registry-operator/pull/1008"},
			},
		},
	},
	"openshift-ingress-canary": {
		prsPerVersion: map[string]versionProgress{
			v416: {
				done: false,
				prs:  []string{"https://github.com/openshift/cluster-ingress-operator/pull/1031"},
			},
		},
	},
	"openshift-ingress-operator": {
		prsPerVersion: map[string]versionProgress{
			v416: {
				done: false,
				prs:  []string{"https://github.com/openshift/cluster-ingress-operator/pull/1031"},
			},
		},
	},
	"openshift-kube-apiserver-operator": {
		runlevel: "yes",
	},
	"openshift-authentication": {
		prsPerVersion: map[string]versionProgress{
			v416: {
				done: true,
				prs:  []string{"https://github.com/openshift/cluster-authentication-operator/pull/656"},
			},
		},
	},
	"openshift-service-ca-operator": {
		prsPerVersion: map[string]versionProgress{
			v416: {
				done: true,
				prs:  []string{"https://github.com/openshift/service-ca-operator/pull/235"},
			},
		},
	},
	"openshift-oauth-apiserver": {
		prsPerVersion: map[string]versionProgress{
			v416: {
				done: true,
				prs:  []string{"https://github.com/openshift/cluster-authentication-operator/pull/656"},
			},
		},
	},
	"openshift-cloud-network-config-controller": {
		prsPerVersion: map[string]versionProgress{
			v417: {
				done: false,
				prs:  []string{"https://github.com/openshift/cluster-network-operator/pull/2282"},
			},
		},
	},
	"openshift-cluster-samples-operator": {
		prsPerVersion: map[string]versionProgress{
			v416: {
				done: true,
				prs:  []string{"https://github.com/openshift/cluster-samples-operator/pull/535"},
			},
		},
	},
	"openshift-kube-storage-version-migrator-operator": {
		prsPerVersion: map[string]versionProgress{
			v416: {
				done: true,
				prs:  []string{"https://github.com/openshift/cluster-kube-storage-version-migrator-operator/pull/107"},
			},
		},
	},
	"openshift-operator-controller": {
		runlevel: "unknown",
		prsPerVersion: map[string]versionProgress{
			v416: {
				done: true,
				prs:  []string{"https://github.com/openshift/operator-framework-operator-controller/pull/100"},
			},
		},
	},
	"openshift-service-ca": {
		prsPerVersion: map[string]versionProgress{
			v416: {
				done: true,
				prs:  []string{"https://github.com/openshift/service-ca-operator/pull/235"},
			},
		},
	},
	"openshift-etcd": {
		runlevel: "yes",
	},
	"openshift-dns": {
		runlevel: "yes",
	},
	"openshift-cluster-csi-drivers": {
		prsPerVersion: map[string]versionProgress{
			v416: {
				done: true,
				prs: []string{
					"https://github.com/openshift/csi-operator/pull/170",
					"https://github.com/openshift/cluster-storage-operator/pull/459",
				},
			},
		},
	},
	"openshift-console-operator": {
		prsPerVersion: map[string]versionProgress{
			v417: {
				done: true,
				prs:  []string{"https://github.com/openshift/console-operator/pull/871"},
			},
			v416: {
				done: false,
				prs:  []string{"https://github.com/openshift/console-operator/pull/908"},
			},
		},
	},
	"openshift-authentication-operator": {
		prsPerVersion: map[string]versionProgress{
			v416: {
				done: true,
				prs:  []string{"https://github.com/openshift/cluster-authentication-operator/pull/656"},
			},
		},
	},
	"openshift-machine-api": {
		prsPerVersion: map[string]versionProgress{
			v416: {
				done: false,
				prs: []string{
					"https://github.com/openshift/cluster-autoscaler-operator/pull/315",
					"https://github.com/openshift/cluster-control-plane-machine-set-operator/pull/282",
					"https://github.com/openshift/machine-api-operator/pull/1220",
					"https://github.com/openshift/machine-api-provider-nutanix/pull/73",
					"https://github.com/openshift/cluster-api-provider-alibaba/pull/50",
				},
			},
			v417: {
				done: false,
				prs: []string{
					"https://github.com/openshift/cluster-baremetal-operator/pull/407",
				},
			},
		},
	},
	"openshift-multus": {
		runlevel: "yes",
	},
	"openshift-network-operator": {
		runlevel: "yes",
	},
	"openshift-apiserver-operator": {
		prsPerVersion: map[string]versionProgress{
			v416: {
				done: true,
				prs:  []string{"https://github.com/openshift/cluster-openshift-apiserver-operator/pull/573"},
			},
		},
	},
	"openshift-kube-scheduler": {
		runlevel: "yes",
	},
	"openshift-cluster-version": {
		prsPerVersion: map[string]versionProgress{
			v416: {
				done: true,
				prs:  []string{"https://github.com/openshift/cluster-version-operator/pull/1038"},
			},
		},
	},
	"openshift-kube-scheduler-operator": {
		runlevel: "yes",
	},
	"openshift-kube-controller-manager": {
		runlevel: "yes",
	},
	"openshift-platform-operators": {
		runlevel: "unknown",
	},
	"openshift-e2e-loki": {
		runlevel: "unknown",
	},
	"openshift-kni-infra":     {},
	"openshift-vsphere-infra": {},
	"openshift-ovirt-infra": {
		noFixNeeded: true,
	},
	"openshift-openstack-infra": {},
	"openshift-nutanix-infra":   {},
	"openshift-cloud-platform-infra": {
		noFixNeeded: true,
	},
	"openshift-apiserver": {
		noFixNeeded: true,
	},
	"openshift-rukpak": {
		runlevel: "unknown",
	},
	"openshift-metallb-system": {
		runlevel: "unknown",
	},
	"openshift-manila-csi-driver": {
		runlevel: "unknown",
	},
	"openshift-kube-proxy": {
		runlevel: "unknown",
	},
	"openshift-sriov-network-operator": {
		runlevel: "unknown",
	},
	"openshift-cluster-api": {
		runlevel: "unknown",
	},
	"openshift-sdn": {
		runlevel: "unknown",
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
	},
	"openshift-config-managed": {
		runlevel: "yes",
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
	"default": {},
	"kube-public": {
		noFixNeeded: true,
	},
	"kube-node-lease": {
		noFixNeeded: true,
	},
	"kube-system": {},
	"oc debug node pods": {
		prsPerVersion: map[string]versionProgress{
			v417: {
				done: true,
				prs:  []string{"https://github.com/openshift/oc/pull/1763"},
			},
		},
	},
}
