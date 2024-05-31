package main

import (
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
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

	version   string
	namespace string
}

type versionProgress struct {
	done      bool
	prs       []string
	sippyTest *SippyTest
}

type nsProgress struct {
	nsName        string
	jiraComponent string

	perVersion map[string]*versionProgress

	runlevel    bool
	nonRunlevel bool
	tested      bool

	noFixNeeded bool
}

type stats struct {
	allPRs  map[string]struct{}
	openPRs map[string]struct{}

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

	sippyFilter = `{"items":[{"columnField":"name","operatorValue":"starts with","value":"[sig-auth] all workloads in ns/"},{"columnField":"name","operatorValue":"ends with","value":"must set the 'openshift.io/required-scc' annotation"}],"linkOperator":"and"}`
)

var (
	out = os.Stdout

	untestedNS = []*nsProgress{}

	versions = []string{v415, v416, v417}

	versionStats = map[string]*stats{
		v415: {},
		v416: {},
		v417: {},
	}
)

func main() {

	// first argument is the filename to write output to (defaults to STDOUT)
	if len(os.Args) > 1 {
		var err error
		out, err = os.Create(os.Args[1])
		if err != nil {
			panic(err)
		}
	}

	for _, v := range versions {
		vstats := versionStats[v]
		vstats.allPRs = make(map[string]struct{})
		vstats.openPRs = make(map[string]struct{})
	}

	fmt.Println("checking status of namespaces and PRs")
	for nsName, ns := range progressPerNs {
		prevDone := false
		for _, v := range versions {
			vstats := versionStats[v]

			if ns.perVersion[v] == nil {
				continue
			}

			for _, pr := range ns.perVersion[v].prs {
				vstats.allPRs[pr] = struct{}{}
			}

			if ns.noFixNeeded {
				vstats.numNoFixNeededNS++
				continue
			} else if ns.perVersion[v].done || prevDone {
				vstats.numDoneNS++
				prevDone = true
				continue
			}

			if ns.runlevel {
				vstats.numRemainingRunlevelNS++
			} else if ns.nonRunlevel {
				vstats.numRemainingNonRunlevelNS++
			}

			allMerged := true
			for _, pr := range ns.perVersion[v].prs {
				if prStatus(pr) == "OPEN" {
					vstats.openPRs[pr] = struct{}{}
					allMerged = false
					break
				}
			}

			if len(ns.perVersion[v].prs) > 0 && allMerged {
				fmt.Printf("* all v%s PRs of %s have been closed\n", v, nsName)
			}
		}
	}

	fmt.Println("\nretrieving sippy tests")
	cntTotalTests := 0
	for _, v := range versions {
		sippyTests := sippyTests(v)
		fmt.Printf("* %d tests for v%s\n", len(sippyTests), v)
		for _, t := range sippyTests {
			nsProgress := progressPerNs[t.namespace]
			nsProgress.tested = true
			nsProgress.nsName = t.namespace

			if len(nsProgress.jiraComponent) > 0 && nsProgress.jiraComponent != t.JiraComponent {
				panic(fmt.Sprintf("jira component changed for ns '%s' from '%s' to '%s'", t.namespace, nsProgress.jiraComponent, t.JiraComponent))
			}
			nsProgress.jiraComponent = t.JiraComponent

			if nsProgress.perVersion == nil {
				nsProgress.perVersion = make(map[string]*versionProgress)
			}

			if nsProgress.perVersion[v] == nil {
				nsProgress.perVersion[v] = &versionProgress{}
			}

			perVersion := nsProgress.perVersion[v]
			perVersion.sippyTest = t
		}
		cntTotalTests += len(sippyTests)
	}
	fmt.Printf("* found %d tests in total\n", cntTotalTests)

	runlevel := make([]*nsProgress, 0)
	nonRunlevel := make([]*nsProgress, 0)
	unknownRunlevel := make([]*nsProgress, 0)

	fmt.Println("\nprocessing namespaces and tests")
	for nsName, ns := range progressPerNs {
		switch {
		case ns.runlevel:
			runlevel = append(runlevel, ns)

		case ns.nonRunlevel:
			nonRunlevel = append(nonRunlevel, ns)

		case !ns.tested:
			untestedNS = append(untestedNS, ns)

		case !ns.runlevel && !ns.nonRunlevel:
			unknownRunlevel = append(unknownRunlevel, ns)
			ns.runlevel, ns.nonRunlevel = getRunlevel(nsName)
			if ns.runlevel || ns.nonRunlevel {
				fmt.Printf("* runlevel:%v nonRunlevel:%v for ns %s\n", ns.runlevel, ns.nonRunlevel, nsName)
			}

		default:
			panic(fmt.Sprintf("cannot categorize ns %s", nsName))
		}

		for _, v := range versions {
			if ns.perVersion == nil || ns.perVersion[v] == nil {
				continue
			}

			if ns.noFixNeeded && ns.perVersion[v].sippyTest.CurrentFlakes > 0 {
				fmt.Printf("* %s %s: no fix needed but is flaking\n", v, nsName)
			}
		}
	}

	header := "|  #  | Component | Namespace | 4.17 Flakes | 4.16 Flakes | 4.15 Flakes | 4.17 PRs | 4.16 PRs | 4.15 PRs |"
	subhdr := "| --- | --------- | --------- | ----------- | ----------- | ----------- | -------- | -------- | -------- |"

	fmt.Fprintf(out, "*Last updated: %s*\n\n", time.Now().Format(time.DateTime))
	fmt.Fprintf(out, "%s\n", getStats())
	fmt.Fprintln(out, "## Non-runlevel")
	fmt.Fprintln(out, header)
	fmt.Fprintln(out, subhdr)
	sortAndPrint(nonRunlevel)

	fmt.Fprintln(out, "\n## Runlevel")
	fmt.Fprintln(out, header)
	fmt.Fprintln(out, subhdr)
	sortAndPrint(runlevel)

	fmt.Fprintln(out, "\n## Unknown runlevel")
	fmt.Fprintln(out, header)
	fmt.Fprintln(out, subhdr)
	sortAndPrint(unknownRunlevel)

	fmt.Fprintln(out, "\n## Untested NS")
	fmt.Fprintln(out, header)
	fmt.Fprintln(out, subhdr)
	sortAndPrint(untestedNS)

	fmt.Fprintln(out, "\n## Jira blob")
	fmt.Fprintf(out, "```\n%s```", jiraBlob())
}

func sippyTests(version string) []*SippyTest {
	sippyReq := fmt.Sprintf("https://sippy.dptools.openshift.org/api/tests?release=%s&filter=%s",
		version,
		url.QueryEscape(sippyFilter),
	)

	resp, err := http.Get(sippyReq)
	if err != nil {
		panic(err)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}

	var tests []*SippyTest
	if err := json.Unmarshal(body, &tests); err != nil {
		panic(err)
	}

	for _, t := range tests {
		t.version = version
		t.namespace = getNamespace(t.Name)
	}

	return tests
}

func sortAndPrint(nsProg []*nsProgress) {
	slices.SortStableFunc(nsProg, func(a, b *nsProgress) int {
		numFlakesA := make([]int, len(versions))
		numFlakesB := make([]int, len(versions))
		for i, v := range versions {
			if a.perVersion == nil || a.perVersion[v] == nil || a.perVersion[v].sippyTest == nil ||
				b.perVersion == nil || b.perVersion[v] == nil || b.perVersion[v].sippyTest == nil {
				continue
			}
			numFlakesA[i] = a.perVersion[v].sippyTest.CurrentFlakes
			numFlakesB[i] = b.perVersion[v].sippyTest.CurrentFlakes
		}

		return cmp.Or(
			cmp.Compare(numFlakesA[2], numFlakesB[2]),
			cmp.Compare(numFlakesA[1], numFlakesB[1]),
			cmp.Compare(numFlakesA[0], numFlakesB[0]),
		)
	})

	for i, ns := range nsProg {
		prLine := map[string]string{v417: "", v416: "", v415: ""}
		prevDone := false
		for _, v := range versions {
			if ns.perVersion == nil || ns.perVersion[v] == nil {
				continue
			}

			status := ""
			if ns.noFixNeeded {
				status = "ready"
			} else if ns.perVersion[v].done {
				status = "DONE; "
			} else if prevDone {
				status = "n/a"
			}

			prs := make([]string, 0)
			for _, pr := range ns.perVersion[v].prs {
				prs = append(prs, fmt.Sprintf("[%s](%s)", prName(pr), pr))
			}

			prLine[v] = fmt.Sprintf("%s%s", status, strings.Join(prs, " "))
			prevDone = ns.perVersion[v].done
		}

		flakes := make(map[string]int)
		for _, v := range versions {
			flakes[v] = 0
			if ns.perVersion[v] != nil && ns.perVersion[v].sippyTest != nil {
				flakes[v] = ns.perVersion[v].sippyTest.CurrentFlakes
			}
		}

		fmt.Fprintf(out, "| %d | %s | %s | %d | %d | %d | %s | %s | %s |\n",
			i+1,
			ns.jiraComponent,
			ns.nsName,
			flakes[v417],
			flakes[v416],
			flakes[v415],
			prLine[v417],
			prLine[v416],
			prLine[v415],
		)
	}
}

func prStatus(url string) string {
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

func getRunlevel(ns string) (runlevel bool, nonRunlevel bool) {
	var out strings.Builder
	cmd := exec.Command("oc", "get", "ns", ns, "-o", "jsonpath='{.metadata.labels.openshift\\.io/run-level}'")
	cmd.Stdout = &out

	if err := cmd.Run(); err != nil {
		return
	}

	if len(strings.ReplaceAll(out.String(), "'", "")) > 0 {
		runlevel = true
		return
	}

	nonRunlevel = true
	return
}

func getStats() string {

	var statsBuf bytes.Buffer
	statsBuf.WriteString("[Authored PRs](https://github.com/pulls?q=is%3Apr+author%3Aliouk+archived%3Afalse+AUTH-482+in%3Atitle+is%3Aopen), [Assigned PRs](https://github.com/pulls?q=is%3Apr+assignee%3Aliouk+archived%3Afalse+AUTH-482+in%3Atitle+is%3Aopen), [All open PRs](https://github.com/search?q=org%3Aopenshift%20is%3Apr%20is%3Aopen%20AUTH-482%20in%3Atitle&type=pullrequests)\n\n[Jira issue](https://issues.redhat.com/browse/AUTH-482)\n\n")
	statsBuf.WriteString("| Version | 4.17 | 4.16 | 4.15 |\n")
	statsBuf.WriteString("| ------- | ---- | ---- | ---- |\n")

	statsBuf.WriteString(fmt.Sprintf("| open PRs | %d/%d | %d/%d | %d/%d |\n",
		len(versionStats[v417].openPRs), len(versionStats[v417].allPRs),
		len(versionStats[v416].openPRs), len(versionStats[v416].allPRs),
		len(versionStats[v415].openPRs), len(versionStats[v415].allPRs),
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
			if progressPerNs[ns].perVersion[v] == nil {
				continue
			}

			status := ""
			if progressPerNs[ns].perVersion[v].done {
				status = "(/) "
			}

			prs := make([]string, 0)
			for _, pr := range progressPerNs[ns].perVersion[v].prs {
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
		nonRunlevel: true,
		perVersion: map[string]*versionProgress{
			v416: {
				done: true,
				prs:  []string{"https://github.com/openshift/cluster-openshift-controller-manager-operator/pull/336"},
			},
		},
	},
	"openshift-etcd-operator": {
		runlevel: true,
	},
	"openshift-insights": {
		nonRunlevel: true,
		perVersion: map[string]*versionProgress{
			v416: {
				done: true,
				prs:  []string{"https://github.com/openshift/insights-operator/pull/915"},
			},
		},
	},
	"openshift-kube-controller-manager-operator": {
		runlevel: true,
	},
	"openshift-ovn-kubernetes": {
		runlevel: true,
	},
	"openshift-console": {
		nonRunlevel: true,
		perVersion: map[string]*versionProgress{
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
		nonRunlevel: true,
		perVersion: map[string]*versionProgress{
			v416: {
				done: true,
				prs:  []string{"https://github.com/openshift/cluster-openshift-controller-manager-operator/pull/336"},
			},
		},
	},
	"openshift-monitoring": {
		nonRunlevel: true,
		perVersion: map[string]*versionProgress{
			v416: {
				done: true,
				prs:  []string{"https://github.com/openshift/cluster-monitoring-operator/pull/2335"},
			},
		},
	},
	"openshift-route-controller-manager": {
		nonRunlevel: true,
		perVersion: map[string]*versionProgress{
			v416: {
				done: true,
				prs:  []string{"https://github.com/openshift/cluster-openshift-controller-manager-operator/pull/336"},
			},
		},
	},
	"openshift-cluster-olm-operator": {
		runlevel:    false,
		nonRunlevel: false,
		perVersion: map[string]*versionProgress{
			v416: {
				done: true,
				prs:  []string{"https://github.com/openshift/cluster-olm-operator/pull/54"},
			},
		},
	},
	"openshift-ingress": {
		nonRunlevel: true,
		perVersion: map[string]*versionProgress{
			v417: {
				done: false,
				prs:  []string{"https://github.com/openshift/cluster-ingress-operator/pull/1031"},
			},
		},
	},
	"openshift-kube-apiserver": {
		runlevel: true,
	},
	"openshift-marketplace": {
		nonRunlevel: true,
		perVersion: map[string]*versionProgress{
			v416: {
				done: true,
				prs:  []string{"https://github.com/operator-framework/operator-marketplace/pull/561"},
			},
		},
	},
	"openshift-network-node-identity": {
		nonRunlevel: true,
		perVersion: map[string]*versionProgress{
			v417: {
				done: false,
				prs:  []string{"https://github.com/openshift/cluster-network-operator/pull/2282"},
			},
		},
	},
	"openshift-operator-lifecycle-manager": {
		nonRunlevel: true,
		perVersion: map[string]*versionProgress{
			v416: {
				done: true,
				prs:  []string{"https://github.com/openshift/operator-framework-olm/pull/703"},
			},
		},
	},
	"openshift-user-workload-monitoring": {
		nonRunlevel: true,
		perVersion: map[string]*versionProgress{
			v416: {
				done: true,
				prs:  []string{"https://github.com/openshift/cluster-monitoring-operator/pull/2335"},
			},
		},
	},
	"openshift-config-operator": {
		nonRunlevel: true,
		perVersion: map[string]*versionProgress{
			v416: {
				done: true,
				prs:  []string{"https://github.com/openshift/cluster-config-operator/pull/410"},
			},
		},
	},
	"openshift-kube-storage-version-migrator": {
		nonRunlevel: true,
		perVersion: map[string]*versionProgress{
			v416: {
				done: true,
				prs:  []string{"https://github.com/openshift/cluster-kube-storage-version-migrator-operator/pull/107"},
			},
		},
	},
	"openshift-cloud-credential-operator": {
		nonRunlevel: true,
		perVersion: map[string]*versionProgress{
			v416: {
				done: true,
				prs:  []string{"https://github.com/openshift/cloud-credential-operator/pull/681"},
			},
		},
	},
	"openshift-cluster-storage-operator": {
		nonRunlevel: true,
		perVersion: map[string]*versionProgress{
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
		nonRunlevel: true,
		perVersion: map[string]*versionProgress{
			v417: {
				done: true,
				prs:  []string{"https://github.com/openshift/machine-config-operator/pull/4219"},
			},
			v416: {
				done: false,
				prs:  []string{"https://github.com/openshift/machine-config-operator/pull/4384"},
			},
		},
	},
	"openshift-cloud-controller-manager": {
		runlevel: true,
	},
	"openshift-dns-operator": {
		runlevel: true,
	},
	"openshift-network-diagnostics": {
		nonRunlevel: true,
		perVersion: map[string]*versionProgress{
			v417: {
				done: false,
				prs:  []string{"https://github.com/openshift/cluster-network-operator/pull/2282"},
			},
		},
	},
	"openshift-cluster-machine-approver": {
		runlevel: true,
	},
	"openshift-catalogd": {
		runlevel:    false,
		nonRunlevel: false,
		perVersion: map[string]*versionProgress{
			v416: {
				done: true,
				prs:  []string{"https://github.com/openshift/operator-framework-catalogd/pull/50"},
			},
		},
	},
	"openshift-cloud-controller-manager-operator": {
		runlevel: true,
	},
	"openshift-cluster-node-tuning-operator": {
		nonRunlevel: true,
		perVersion: map[string]*versionProgress{
			v416: {
				done: true,
				prs:  []string{"https://github.com/openshift/cluster-node-tuning-operator/pull/968"},
			},
		},
	},
	"openshift-image-registry": {
		nonRunlevel: true,
		perVersion: map[string]*versionProgress{
			v416: {
				done: true,
				prs:  []string{"https://github.com/openshift/cluster-image-registry-operator/pull/1008"},
			},
		},
	},
	"openshift-ingress-canary": {
		nonRunlevel: true,
		perVersion: map[string]*versionProgress{
			v417: {
				done: false,
				prs:  []string{"https://github.com/openshift/cluster-ingress-operator/pull/1031"},
			},
		},
	},
	"openshift-ingress-operator": {
		nonRunlevel: true,
		perVersion: map[string]*versionProgress{
			v417: {
				done: false,
				prs:  []string{"https://github.com/openshift/cluster-ingress-operator/pull/1031"},
			},
		},
	},
	"openshift-kube-apiserver-operator": {
		runlevel: true,
	},
	"openshift-authentication": {
		nonRunlevel: true,
		perVersion: map[string]*versionProgress{
			v416: {
				done: true,
				prs:  []string{"https://github.com/openshift/cluster-authentication-operator/pull/656"},
			},
			v415: {
				done: false,
				prs:  []string{"https://github.com/openshift/cluster-authentication-operator/pull/675"},
			},
		},
	},
	"openshift-service-ca-operator": {
		nonRunlevel: true,
		perVersion: map[string]*versionProgress{
			v416: {
				done: true,
				prs:  []string{"https://github.com/openshift/service-ca-operator/pull/235"},
			},
			v415: {
				done: false,
				prs:  []string{"https://github.com/openshift/service-ca-operator/pull/243"},
			},
		},
	},
	"openshift-oauth-apiserver": {
		nonRunlevel: true,
		perVersion: map[string]*versionProgress{
			v416: {
				done: true,
				prs:  []string{"https://github.com/openshift/cluster-authentication-operator/pull/656"},
			},
			v415: {
				done: false,
				prs:  []string{"https://github.com/openshift/cluster-authentication-operator/pull/675"},
			},
		},
	},
	"openshift-cloud-network-config-controller": {
		nonRunlevel: true,
		perVersion: map[string]*versionProgress{
			v417: {
				done: false,
				prs:  []string{"https://github.com/openshift/cluster-network-operator/pull/2282"},
			},
		},
	},
	"openshift-cluster-samples-operator": {
		nonRunlevel: true,
		perVersion: map[string]*versionProgress{
			v416: {
				done: true,
				prs:  []string{"https://github.com/openshift/cluster-samples-operator/pull/535"},
			},
		},
	},
	"openshift-kube-storage-version-migrator-operator": {
		nonRunlevel: true,
		perVersion: map[string]*versionProgress{
			v416: {
				done: true,
				prs:  []string{"https://github.com/openshift/cluster-kube-storage-version-migrator-operator/pull/107"},
			},
		},
	},
	"openshift-operator-controller": {
		runlevel:    false,
		nonRunlevel: false,
		perVersion: map[string]*versionProgress{
			v416: {
				done: true,
				prs:  []string{"https://github.com/openshift/operator-framework-operator-controller/pull/100"},
			},
		},
	},
	"openshift-service-ca": {
		nonRunlevel: true,
		perVersion: map[string]*versionProgress{
			v416: {
				done: true,
				prs:  []string{"https://github.com/openshift/service-ca-operator/pull/235"},
			},
			v415: {
				done: false,
				prs:  []string{"https://github.com/openshift/service-ca-operator/pull/243"},
			},
		},
	},
	"openshift-etcd": {
		runlevel: true,
	},
	"openshift-dns": {
		runlevel: true,
	},
	"openshift-cluster-csi-drivers": {
		nonRunlevel: true,
		perVersion: map[string]*versionProgress{
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
		nonRunlevel: true,
		perVersion: map[string]*versionProgress{
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
		nonRunlevel: true,
		perVersion: map[string]*versionProgress{
			v416: {
				done: true,
				prs:  []string{"https://github.com/openshift/cluster-authentication-operator/pull/656"},
			},
			v415: {
				done: false,
				prs:  []string{"https://github.com/openshift/cluster-authentication-operator/pull/675"},
			},
		},
	},
	"openshift-machine-api": {
		nonRunlevel: true,
		perVersion: map[string]*versionProgress{
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
		runlevel: true,
	},
	"openshift-network-operator": {
		runlevel: true,
	},
	"openshift-apiserver-operator": {
		nonRunlevel: true,
		perVersion: map[string]*versionProgress{
			v416: {
				done: true,
				prs:  []string{"https://github.com/openshift/cluster-openshift-apiserver-operator/pull/573"},
			},
		},
	},
	"openshift-kube-scheduler": {
		runlevel: true,
	},
	"openshift-cluster-version": {
		nonRunlevel: true,
		perVersion: map[string]*versionProgress{
			v416: {
				done: true,
				prs:  []string{"https://github.com/openshift/cluster-version-operator/pull/1038"},
			},
		},
	},
	"openshift-kube-scheduler-operator": {
		runlevel: true,
	},
	"openshift-kube-controller-manager": {
		runlevel: true,
	},
	"openshift-platform-operators": {
		runlevel:    false,
		nonRunlevel: false,
	},
	"openshift-e2e-loki": {
		runlevel:    false,
		nonRunlevel: false,
	},
	"openshift-kni-infra": {
		nonRunlevel: true,
	},
	"openshift-vsphere-infra": {
		nonRunlevel: true,
	},
	"openshift-ovirt-infra": {
		noFixNeeded: true,
	},
	"openshift-openstack-infra": {
		nonRunlevel: true,
	},
	"openshift-nutanix-infra": {
		nonRunlevel: true,
	},
	"openshift-cloud-platform-infra": {
		noFixNeeded: true,
	},
	"openshift-apiserver": {
		noFixNeeded: true,
	},
	"openshift-rukpak": {
		runlevel:    false,
		nonRunlevel: false,
	},
	"openshift-metallb-system": {
		runlevel:    false,
		nonRunlevel: false,
	},
	"openshift-manila-csi-driver": {
		runlevel:    false,
		nonRunlevel: false,
	},
	"openshift-kube-proxy": {
		runlevel:    false,
		nonRunlevel: false,
	},
	"openshift-sriov-network-operator": {
		runlevel:    false,
		nonRunlevel: false,
	},
	"openshift-cluster-api": {
		runlevel:    false,
		nonRunlevel: false,
	},
	"openshift-sdn": {
		runlevel:    false,
		nonRunlevel: false,
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
		runlevel: true,
	},
	"openshift-config-managed": {
		runlevel: true,
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
		runlevel: true,
	},
	"kube-public": {
		noFixNeeded: true,
	},
	"kube-node-lease": {
		noFixNeeded: true,
	},
	"kube-system": {},
	"oc debug node pods": {
		nonRunlevel: true,
		perVersion: map[string]*versionProgress{
			v417: {
				done: true,
				prs:  []string{"https://github.com/openshift/oc/pull/1763"},
			},
		},
	},
}
