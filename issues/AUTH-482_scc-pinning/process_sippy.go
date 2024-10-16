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
	done         bool
	prs          []string
	sippyTest    *SippyTest
	wontdoReason string
	noFixNeeded  bool
}

type nsProgress struct {
	nsName        string
	jiraComponent string

	perVersion map[string]*versionProgress

	runlevel    bool
	nonRunlevel bool

	noFixNeeded bool
}

type stats struct {
	allPRs  map[string]struct{}
	openPRs map[string]struct{}

	numNS                         int
	numDoneNS                     int
	numNoFixNeededNS              int
	numRemainingRunlevelNS        int
	numRemainingNonRunlevelNS     int
	numRemainingUnknownRunlevelNS int
}

const (
	v415 = "4.15"
	v416 = "4.16"
	v417 = "4.17"
	v418 = "4.18"

	sippyFilter = `{"items":[{"columnField":"name","operatorValue":"starts with","value":"[sig-auth] all workloads in ns/"},{"columnField":"name","operatorValue":"ends with","value":"must set the 'openshift.io/required-scc' annotation"}],"linkOperator":"and"}`
)

var (
	out = os.Stdout

	versions = []string{v415, v416, v417, v418}

	untestedPerVersion = map[string][]string{
		v415: {},
		v416: {},
		v417: {},
		v418: {},
	}

	versionStats = map[string]*stats{
		v415: {allPRs: make(map[string]struct{}), openPRs: make(map[string]struct{})},
		v416: {allPRs: make(map[string]struct{}), openPRs: make(map[string]struct{})},
		v417: {allPRs: make(map[string]struct{}), openPRs: make(map[string]struct{})},
		v418: {allPRs: make(map[string]struct{}), openPRs: make(map[string]struct{})},
	}

	ignoreNS = map[string]struct{}{
		"openshift-rukpak": {}, // see https://github.com/openshift/operator-framework-rukpak/pull/92#issuecomment-2286534684
	}

	branchedAt417 time.Time
)

func init() {
	zh, err := time.LoadLocation("Europe/Zurich")
	if err != nil {
		panic(err)
	}
	branchedAt417 = time.Date(2024, 8, 10, 0, 0, 0, 0, zh)
}

func main() {

	// first argument is the filename to write output to (defaults to STDOUT)
	if len(os.Args) > 1 {
		var err error
		out, err = os.Create(os.Args[1])
		if err != nil {
			panic(err)
		}
	}

	fmt.Println("checking status of namespaces and PRs")
	for nsName, ns := range progressPerNS {
		prevDone := false
		for _, v := range versions {
			vstats := versionStats[v]
			vstats.numNS = len(progressPerNS)

			if ns.perVersion == nil {
				ns.perVersion = make(map[string]*versionProgress)
			}

			if ns.perVersion[v] == nil {
				ns.perVersion[v] = &versionProgress{}
				// mark next versions as done
				if prevDone {
					ns.perVersion[v].done = true
				}
			}

			if ns.perVersion[v] != nil {
				for _, pr := range ns.perVersion[v].prs {
					vstats.allPRs[pr] = struct{}{}
				}

				// check which PRs merged after 4.17
				// if v == v417 {
				// 	for _, pr := range ns.perVersion[v].prs {
				// 		mergedAt := prMergedAt(pr)
				// 		if !mergedAt.IsZero() && mergedAt.After(branchedAt417) {
				// 			fmt.Printf("* merged after v4.17: %s (mergedAt = %s)\n", pr, mergedAt)
				// 		}
				// 	}
				// }
			}

			if ns.noFixNeeded {
				vstats.numNoFixNeededNS++
				continue
			} else if ns.perVersion[v] != nil && ns.perVersion[v].done {
				vstats.numDoneNS++
				prevDone = true
				continue
			}

			if ns.runlevel {
				vstats.numRemainingRunlevelNS++
			} else if ns.nonRunlevel {
				vstats.numRemainingNonRunlevelNS++
			} else {
				vstats.numRemainingUnknownRunlevelNS++
			}

			if ns.perVersion[v] == nil {
				continue
			}

			allMerged := true
			for _, pr := range ns.perVersion[v].prs {
				if prStatus(pr) == "OPEN" {
					// warn about 4.17 PRs that will now merge on 4.18
					// if v == v417 {
					// 	fmt.Printf("* v4.17 PR should be promoted to 4.18: %s\n", pr)
					// }
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
	versionTestStats := map[string]*struct {
		total     int
		successes int
		flakes    int
		failures  int
	}{
		v415: {},
		v416: {},
		v417: {},
		v418: {},
	}

	for _, v := range versions {
		sippyTests := sippyTests(v)
		fmt.Printf("* %d tests for v%s\n", len(sippyTests), v)
		for _, t := range sippyTests {
			nsProgress := progressPerNS[t.namespace]

			if _, ignore := ignoreNS[t.namespace]; ignore || nsProgress == nil {
				continue
			}

			nsProgress.nsName = t.namespace

			// count only non-runlevel NS tests
			if nsProgress.nonRunlevel {
				testStats := versionTestStats[v]
				testStats.total += t.CurrentRuns
				testStats.successes += t.CurrentSuccesses
				testStats.flakes += t.CurrentFlakes
				testStats.failures += t.CurrentFailures
			}

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
	for nsName, ns := range progressPerNS {
		ns.nsName = nsName
		switch {
		case ns.runlevel:
			runlevel = append(runlevel, ns)

		case ns.nonRunlevel:
			nonRunlevel = append(nonRunlevel, ns)

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
			if ns.perVersion == nil || ns.perVersion[v] == nil || ns.perVersion[v].sippyTest == nil {
				untestedPerVersion[v] = append(untestedPerVersion[v], ns.nsName)
				continue
			}

			if ns.perVersion[v].done && ns.perVersion[v].sippyTest.CurrentFlakes > 0 {
				fmt.Printf("* %s %s: namespace completed but is flaking\n", v, nsName)
			}

			if !ns.noFixNeeded && (!ns.perVersion[v].done && ns.perVersion[v].sippyTest.CurrentFlakes == 0) {
				fmt.Printf("* %s %s: namespace marked as incomplete but has no flakes\n", v, nsName)
			}

			if ns.noFixNeeded && ns.perVersion[v].sippyTest.CurrentFlakes > 0 {
				fmt.Printf("* %s %s: no fix needed but is flaking\n", v, nsName)
			}
		}
	}

	header := "|  #  | Component | Namespace | 4.18 Flakes | 4.17 Flakes | 4.16 Flakes | 4.15 Flakes | 4.18 PRs | 4.17 PRs | 4.16 PRs | 4.15 PRs |"
	subhdr := "| --- | --------- | --------- | ----------- | ----------- | ----------- | ----------- | -------- | -------- | -------- | -------- |"
	statsStr := getStats()

	fmt.Fprintf(out, "*Last updated: %s*\n\n", time.Now().Format(time.DateTime))
	fmt.Fprint(out, "[Authored PRs](https://github.com/pulls?q=is%3Apr+author%3Aliouk+archived%3Afalse+AUTH-482+in%3Atitle+is%3Aopen), [Assigned PRs](https://github.com/pulls?q=is%3Apr+assignee%3Aliouk+archived%3Afalse+AUTH-482+in%3Atitle+is%3Aopen), [All open PRs](https://github.com/search?q=org%3Aopenshift+is%3Apr+is%3Aopen+%2Fset+required-scc+for+openshift+workloads%2F+in%3Atitle&type=pullrequests)\n\n[Jira issue](https://issues.redhat.com/browse/AUTH-482) [Jira Backport Dashboard](https://issues.redhat.com/secure/Dashboard.jspa?selectPageId=12363204)\n\n")

	fmt.Fprintf(out, "| Tests | 4.18 | 4.17 | 4.16 | 4.15 |\n")
	fmt.Fprintf(out, "| ----- | ---- | ---- | ---- | ---- |\n")
	fmt.Fprintf(out, "| total runs | %d | %d | %d | %d |\n",
		versionTestStats[v418].total,
		versionTestStats[v417].total,
		versionTestStats[v416].total,
		versionTestStats[v415].total,
	)
	fmt.Fprintf(out, "| successes | %d | %d | %d | %d |\n",
		versionTestStats[v418].successes,
		versionTestStats[v417].successes,
		versionTestStats[v416].successes,
		versionTestStats[v415].successes,
	)
	fmt.Fprintf(out, "| flakes | %d | %d | %d | %d |\n",
		versionTestStats[v418].flakes,
		versionTestStats[v417].flakes,
		versionTestStats[v416].flakes,
		versionTestStats[v415].flakes,
	)
	fmt.Fprintf(out, "| failures | %d | %d | %d | %d |\n\n",
		versionTestStats[v418].failures,
		versionTestStats[v417].failures,
		versionTestStats[v416].failures,
		versionTestStats[v415].failures,
	)

	fmt.Fprintf(out, "| Num PRs | 4.18 | 4.17 | 4.16 | 4.15 |\n")
	fmt.Fprintf(out, "| ------- | ---- | ---- | ---- | ---- |\n")
	fmt.Fprintf(out, "| open PRs | %d | %d | %d | %d |\n", len(versionStats[v418].openPRs), len(versionStats[v417].openPRs), len(versionStats[v416].openPRs), len(versionStats[v415].openPRs))
	fmt.Fprintf(out, "| total PRs | %d | %d | %d | %d |\n\n", len(versionStats[v418].allPRs), len(versionStats[v417].allPRs), len(versionStats[v416].allPRs), len(versionStats[v415].allPRs))

	fmt.Fprintf(out, "| # namespaces | 4.18 | 4.17 | 4.16 | 4.15 |\n")
	fmt.Fprintf(out, "| ------ | ---- | ---- | ---- | ---- |\n")
	fmt.Fprintf(out, "%s\n", statsStr)
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
	sortAndPrintUntested(out)

	fmt.Fprintln(out, "\n## Jira blob")
	fmt.Fprintf(out, "```\n%s```", jiraBlob(statsStr))
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

func sortAndPrintUntested(out *os.File) {
	for _, v := range versions {
		fmt.Fprintf(out, "### %s\n", v)
		fmt.Fprintf(out, "| ns  |\n")
		fmt.Fprintf(out, "| --- |\n")
		for _, ns := range untestedPerVersion[v] {
			fmt.Fprintf(out, "| %s |\n", ns)
		}
	}
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
		prLine := map[string]string{v418: "", v417: "", v416: "", v415: ""}
		for _, v := range versions {
			if ns.perVersion == nil || ns.perVersion[v] == nil {
				continue
			}

			status := ""
			if ns.noFixNeeded {
				status = "ready"
			} else if len(ns.perVersion[v].wontdoReason) > 0 {
				status = fmt.Sprintf("wontdo: %s", ns.perVersion[v].wontdoReason)
			} else if ns.perVersion[v].done {
				status = "DONE; "
			} else if ns.perVersion[v].noFixNeeded {
				status = "n/a"
			}

			prs := make([]string, 0)
			for _, pr := range ns.perVersion[v].prs {
				prs = append(prs, fmt.Sprintf("[%s](%s)", prName(pr), pr))
			}

			prLine[v] = fmt.Sprintf("%s%s", status, strings.Join(prs, " "))
		}

		flakes := make(map[string]int)
		for _, v := range versions {
			flakes[v] = 0
			if ns.perVersion[v] != nil && ns.perVersion[v].sippyTest != nil {
				flakes[v] = ns.perVersion[v].sippyTest.CurrentFlakes
			}
		}

		fmt.Fprintf(out, "| %d | %s | %s | %d | %d | %d | %d | %s | %s | %s | %s |\n",
			i+1,
			ns.jiraComponent,
			ns.nsName,
			flakes[v418],
			flakes[v417],
			flakes[v416],
			flakes[v415],
			prLine[v418],
			prLine[v417],
			prLine[v416],
			prLine[v415],
		)
	}
}

func prMergedAt(url string) time.Time {
	matches := regexp.MustCompile(`https\:\/\/github\.com\/(.*)\/pull\/(\d+)`).FindStringSubmatch(url)
	if len(matches) != 3 {
		panic("bad PR url format: " + url)
	}

	var out, stderr strings.Builder
	cmd := exec.Command("gh", "pr", "view", matches[2], "--repo", matches[1], "--json", "mergedAt", "-q", ".mergedAt")
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		panic(fmt.Errorf("cmd '%s' failed with error: %s", cmd.String(), stderr.String()))
	}

	mergedAtStr := strings.TrimSpace(out.String())
	if len(mergedAtStr) == 0 {
		return time.Time{}
	}

	mergedAt, err := time.Parse("2006-01-02T15:04:05Z", mergedAtStr)
	if err != nil {
		panic(fmt.Errorf("couldn't parse time string '%s': %v", mergedAtStr, err))
	}

	return mergedAt
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

	statsBuf.WriteString(fmt.Sprintf("| monitored | %d | %d | %d | %d |\n",
		versionStats[v418].numNS,
		versionStats[v417].numNS,
		versionStats[v416].numNS,
		versionStats[v415].numNS,
	))
	statsBuf.WriteString(fmt.Sprintf("| fix needed | %d | %d | %d | %d |\n",
		versionStats[v418].numNS-versionStats[v418].numNoFixNeededNS,
		versionStats[v417].numNS-versionStats[v417].numNoFixNeededNS,
		versionStats[v416].numNS-versionStats[v416].numNoFixNeededNS,
		versionStats[v415].numNS-versionStats[v415].numNoFixNeededNS,
	))
	statsBuf.WriteString(fmt.Sprintf("| fixed | %d | %d | %d | %d |\n",
		versionStats[v418].numDoneNS, versionStats[v417].numDoneNS, versionStats[v416].numDoneNS, versionStats[v415].numDoneNS,
	))
	statsBuf.WriteString(fmt.Sprintf("| remaining | %d | %d | %d | %d |\n",
		versionStats[v418].numNS-(versionStats[v418].numDoneNS+versionStats[v418].numNoFixNeededNS),
		versionStats[v417].numNS-(versionStats[v417].numDoneNS+versionStats[v417].numNoFixNeededNS),
		versionStats[v417].numNS-(versionStats[v416].numDoneNS+versionStats[v416].numNoFixNeededNS),
		versionStats[v417].numNS-(versionStats[v415].numDoneNS+versionStats[v415].numNoFixNeededNS),
	))
	statsBuf.WriteString(fmt.Sprintf("| ~ remaining non-runlevel | %d | %d | %d | %d |\n",
		// untested namespaces end up being counted as remaining-non-runlevel
		versionStats[v418].numRemainingNonRunlevelNS,
		versionStats[v417].numRemainingNonRunlevelNS,
		versionStats[v416].numRemainingNonRunlevelNS,
		versionStats[v415].numRemainingNonRunlevelNS,
	))
	statsBuf.WriteString(fmt.Sprintf("| ~ remaining runlevel (low-prio) | %d | %d | %d | %d |\n",
		versionStats[v418].numRemainingRunlevelNS,
		versionStats[v417].numRemainingRunlevelNS,
		versionStats[v416].numRemainingRunlevelNS,
		versionStats[v415].numRemainingRunlevelNS,
	))
	statsBuf.WriteString(fmt.Sprintf("| ~ untested | %d | %d | %d | %d |\n",
		len(untestedPerVersion[v418]),
		len(untestedPerVersion[v417]),
		len(untestedPerVersion[v416]),
		len(untestedPerVersion[v415]),
	))

	return statsBuf.String()
}

func jiraBlob(statsStr string) string {
	nses := make([]string, 0, len(progressPerNS))
	for ns := range progressPerNS {
		nses = append(nses, ns)
	}

	slices.Sort(nses)

	var jiraBlob bytes.Buffer
	jiraBlob.WriteString("h3. Progress summary\n")
	jiraBlob.WriteString("|| \\# namespaces || 4.18 || 4.17 || 4.16 || 4.15 ||\n")
	jiraBlob.WriteString(fmt.Sprintf("%s\n", statsStr))
	jiraBlob.WriteString("h3. Progress breakdown\n")
	jiraBlob.WriteString("||#||namespace||4.18||4.17||4.16||4.15||\n")

	slices.SortStableFunc(nses, func(ns1, ns2 string) int {
		if progressPerNS[ns1].runlevel {
			return 1
		}

		if progressPerNS[ns2].runlevel {
			return -1
		}

		return 0
	})

	i := 1
	for _, ns := range nses {
		nsProg := progressPerNS[ns]

		if nsProg.noFixNeeded {
			continue
		}

		prLine := map[string]string{
			v418: "",
			v417: "",
			v416: "",
			v415: "",
		}

		for _, v := range versions {
			if nsProg.perVersion[v] == nil {
				continue
			}

			if len(nsProg.perVersion[v].wontdoReason) > 0 || nsProg.perVersion[v].noFixNeeded {
				prLine[v] = "n/a"
				continue
			}

			status := ""
			if nsProg.perVersion[v].done {
				status = "(/) "
			}

			prs := make([]string, 0)
			for _, pr := range nsProg.perVersion[v].prs {
				prs = append(prs, fmt.Sprintf("[%s|%s]", prName(pr), pr))
			}
			prLine[v] = fmt.Sprintf("%s%s", status, strings.Join(prs, " "))
		}

		nsName := ns
		if nsProg.runlevel {
			nsName = "(runlevel) " + ns
		}

		jiraBlob.WriteString(fmt.Sprintf("| %d | %s | %s | %s | %s | %s |\n",
			i,
			nsName,
			prLine[v418],
			prLine[v417],
			prLine[v416],
			prLine[v415],
		))
		i++
	}

	return jiraBlob.String()
}

func prName(url string) string {
	parts := strings.Split(url, "/")
	return fmt.Sprintf("#%s", parts[len(parts)-1])
}

var progressPerNS = map[string]*nsProgress{
	"openshift-controller-manager-operator": {
		nonRunlevel: true,
		perVersion: map[string]*versionProgress{
			v416: {
				done: true,
				prs:  []string{"https://github.com/openshift/cluster-openshift-controller-manager-operator/pull/336"},
			},
			v415: {
				done: true,
				prs:  []string{"https://github.com/openshift/cluster-openshift-controller-manager-operator/pull/361"},
			},
		},
	},
	"openshift-etcd-operator": {
		runlevel: true,
	},
	"openshift-insights": {
		nonRunlevel: true,
		perVersion: map[string]*versionProgress{
			v418: {done: false},
			v417: {done: false},
			v416: {
				done: false,
				prs:  []string{"https://github.com/openshift/insights-operator/pull/915"},
			},
			v415: {
				done: true,
				prs:  []string{"https://github.com/openshift/insights-operator/pull/967"},
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
				done: true,
				prs:  []string{"https://github.com/openshift/console-operator/pull/908"},
			},
			v415: {
				done: true,
				prs:  []string{"https://github.com/openshift/console-operator/pull/924"},
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
			v415: {
				done: true,
				prs:  []string{"https://github.com/openshift/cluster-openshift-controller-manager-operator/pull/361"},
			},
		},
	},
	"openshift-monitoring": {
		nonRunlevel: true,
		perVersion: map[string]*versionProgress{
			v418: {done: false},
			v417: {done: false},
			v416: {
				done: false,
				prs:  []string{"https://github.com/openshift/cluster-monitoring-operator/pull/2335"},
			},
			v415: {
				done: true,
				prs:  []string{"https://github.com/openshift/cluster-monitoring-operator/pull/2420"},
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
			v415: {
				done: true,
				prs:  []string{"https://github.com/openshift/cluster-openshift-controller-manager-operator/pull/361"},
			},
		},
	},
	"openshift-cluster-olm-operator": {
		nonRunlevel: true,
		perVersion: map[string]*versionProgress{
			v416: {
				done: true,
				prs:  []string{"https://github.com/openshift/cluster-olm-operator/pull/54"},
			},
			v415: {
				done:         true,
				wontdoReason: "TechPreview", // respective feature was on TechPreview on 4.15
			},
		},
	},
	"openshift-ingress": {
		nonRunlevel: true,
		perVersion: map[string]*versionProgress{
			v418: {
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
			v418: {
				done: true,
				prs:  []string{"https://github.com/operator-framework/operator-marketplace/pull/578"},
			},
			v417: {done: false},
			v416: {
				done: false,
				prs:  []string{"https://github.com/operator-framework/operator-marketplace/pull/561"},
			},
			v415: {
				done: true,
				prs:  []string{"https://github.com/operator-framework/operator-marketplace/pull/570"},
			},
		},
	},
	"openshift-network-console": {
		nonRunlevel: true,
		perVersion: map[string]*versionProgress{
			v418: {done: false},
			v417: {done: false},
			v416: {done: false},
			v415: {done: false},
		},
	},
	"openshift-network-node-identity": {
		nonRunlevel: true,
		perVersion: map[string]*versionProgress{
			v418: {
				done: true,
				prs:  []string{"https://github.com/openshift/cluster-network-operator/pull/2282"},
			},
			v417: {
				done: true,
				prs:  []string{"https://github.com/openshift/cluster-network-operator/pull/2490"},
			},
			v416: {
				done: false,
				prs:  []string{"https://github.com/openshift/cluster-network-operator/pull/2496"},
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
			v415: {
				done: true,
				prs:  []string{"https://github.com/openshift/operator-framework-olm/pull/828"},
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
			v415: {
				done: true,
				prs:  []string{"https://github.com/openshift/cluster-monitoring-operator/pull/2420"},
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
			v415: {
				done: true,
				prs:  []string{"https://github.com/openshift/cluster-config-operator/pull/420"},
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
			v415: {
				done: true,
				prs:  []string{"https://github.com/openshift/cluster-kube-storage-version-migrator-operator/pull/112"},
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
			v415: {
				done: true,
				prs:  []string{"https://github.com/openshift/cloud-credential-operator/pull/736"},
			},
		},
	},
	"openshift-cluster-storage-operator": {
		nonRunlevel: true,
		perVersion: map[string]*versionProgress{
			v418: {
				done: true,
				prs:  []string{"https://github.com/openshift/cluster-storage-operator/pull/516"},
			},
			v417: {done: false},
			v416: {
				done: false,
				prs: []string{
					"https://github.com/openshift/cluster-storage-operator/pull/459",
					"https://github.com/openshift/cluster-csi-snapshot-controller-operator/pull/196",
				},
			},
			v415: {
				done: true,
				prs: []string{
					"https://github.com/openshift/cluster-storage-operator/pull/484",
					"https://github.com/openshift/cluster-csi-snapshot-controller-operator/pull/211",
				},
			},
		},
	},
	"openshift-machine-config-operator": {
		nonRunlevel: true,
		perVersion: map[string]*versionProgress{
			v418: {
				done: false,
				prs:  []string{"https://github.com/openshift/machine-config-operator/pull/4636"},
			},
			v417: {
				done: false,
				prs:  []string{"https://github.com/openshift/machine-config-operator/pull/4219"},
			},
			v416: {
				done: false,
				prs:  []string{"https://github.com/openshift/machine-config-operator/pull/4384"},
			},
			v415: {
				done: true,
				prs:  []string{"https://github.com/openshift/machine-config-operator/pull/4393"},
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
			v418: {
				done: true,
				prs:  []string{"https://github.com/openshift/cluster-network-operator/pull/2282"},
			},
			v417: {
				done: true,
				prs:  []string{"https://github.com/openshift/cluster-network-operator/pull/2490"},
			},
			v416: {
				done: false,
				prs:  []string{"https://github.com/openshift/cluster-network-operator/pull/2496"},
			},
		},
	},
	"openshift-cluster-machine-approver": {
		runlevel: true,
	},
	"openshift-catalogd": {
		nonRunlevel: true,
		perVersion: map[string]*versionProgress{
			v416: {
				done: true,
				prs:  []string{"https://github.com/openshift/operator-framework-catalogd/pull/50"},
			},
			v415: {
				done: true,
				prs:  []string{"https://github.com/openshift/operator-framework-catalogd/pull/58"},
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
			v415: {
				done: true,
				prs:  []string{"https://github.com/openshift/cluster-node-tuning-operator/pull/1117"},
			},
		},
	},
	"openshift-image-registry": {
		nonRunlevel: true,
		perVersion: map[string]*versionProgress{
			v418: {done: false},
			v417: {done: false},
			v416: {
				done: false,
				prs:  []string{"https://github.com/openshift/cluster-image-registry-operator/pull/1008"},
			},
			v415: {
				done: true,
				prs:  []string{"https://github.com/openshift/cluster-image-registry-operator/pull/1067"},
			},
		},
	},
	"openshift-ingress-canary": {
		nonRunlevel: true,
		perVersion: map[string]*versionProgress{
			v418: {
				done: false,
				prs:  []string{"https://github.com/openshift/cluster-ingress-operator/pull/1031"},
			},
		},
	},
	"openshift-ingress-operator": {
		nonRunlevel: true,
		perVersion: map[string]*versionProgress{
			v418: {
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
				done: true,
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
				done: true,
				prs:  []string{"https://github.com/openshift/service-ca-operator/pull/243"},
			},
		},
	},
	"openshift-storage": {
		nonRunlevel: true,
		perVersion: map[string]*versionProgress{
			v418: {done: false},
			v417: {done: false},
			v416: {done: false},
			v415: {done: false},
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
				done: true,
				prs:  []string{"https://github.com/openshift/cluster-authentication-operator/pull/675"},
			},
		},
	},
	"openshift-cloud-network-config-controller": {
		nonRunlevel: true,
		perVersion: map[string]*versionProgress{
			v418: {
				done: true,
				prs:  []string{"https://github.com/openshift/cluster-network-operator/pull/2282"},
			},
			v417: {
				done: true,
				prs:  []string{"https://github.com/openshift/cluster-network-operator/pull/2490"},
			},
			v416: {
				done: false,
				prs:  []string{"https://github.com/openshift/cluster-network-operator/pull/2496"},
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
			v415: {
				done: true,
				prs:  []string{"https://github.com/openshift/cluster-samples-operator/pull/548"},
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
			v415: {
				done: true,
				prs:  []string{"https://github.com/openshift/cluster-kube-storage-version-migrator-operator/pull/112"},
			},
		},
	},
	"openshift-operator-controller": {
		nonRunlevel: true,
		perVersion: map[string]*versionProgress{
			v416: {
				done: true,
				prs:  []string{"https://github.com/openshift/operator-framework-operator-controller/pull/100"},
			},
			v415: {
				done: true,
				prs:  []string{"https://github.com/openshift/operator-framework-operator-controller/pull/120"},
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
				done: true,
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
			v418: {
				done: false,
				prs: []string{
					"https://github.com/openshift/cluster-storage-operator/pull/524",
					"https://github.com/openshift/gcp-pd-csi-driver-operator/pull/131",
					"https://github.com/openshift/kubevirt-csi-driver-operator/pull/6",
					"https://github.com/openshift/azure-disk-csi-driver-operator/pull/127",
					"https://github.com/openshift/azure-file-csi-driver-operator/pull/108",
				},
			},
			v417: {done: false},
			v416: {
				done: false,
				prs: []string{
					"https://github.com/openshift/csi-operator/pull/170",
					"https://github.com/openshift/cluster-storage-operator/pull/459",
				},
			},
			v415: {
				done: true,
				prs: []string{
					"https://github.com/openshift/cluster-storage-operator/pull/484",
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
				done: true,
				prs:  []string{"https://github.com/openshift/console-operator/pull/908"},
			},
			v415: {
				done: true,
				prs:  []string{"https://github.com/openshift/console-operator/pull/924"},
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
				done: true,
				prs:  []string{"https://github.com/openshift/cluster-authentication-operator/pull/675"},
			},
		},
	},
	"openshift-machine-api": {
		nonRunlevel: true,
		perVersion: map[string]*versionProgress{
			v418: {done: false},
			v417: {
				done: false,
				prs: []string{
					"https://github.com/openshift/cluster-baremetal-operator/pull/407",
				},
			},
			v416: {
				done: false,
				prs: []string{
					"https://github.com/openshift/cluster-autoscaler-operator/pull/315",
					"https://github.com/openshift/cluster-control-plane-machine-set-operator/pull/282",
					"https://github.com/openshift/machine-api-operator/pull/1220",
					"https://github.com/openshift/machine-api-provider-nutanix/pull/73",
					"https://github.com/openshift/cluster-api-provider-alibaba/pull/50",
					"https://github.com/openshift/cluster-baremetal-operator/pull/433",
				},
			},
			v415: {
				done: true,
				prs: []string{
					"https://github.com/openshift/cluster-autoscaler-operator/pull/332",
					"https://github.com/openshift/cluster-control-plane-machine-set-operator/pull/326",
					"https://github.com/openshift/machine-api-operator/pull/1288",
					"https://github.com/openshift/machine-api-provider-nutanix/pull/81",
					"https://github.com/openshift/cluster-api-provider-alibaba/pull/57",
					"https://github.com/openshift/cluster-baremetal-operator/pull/443",
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
			v415: {
				done: true,
				prs:  []string{"https://github.com/openshift/cluster-openshift-apiserver-operator/pull/581"},
			},
		},
	},
	"openshift-kube-scheduler": {
		runlevel: true,
	},
	"openshift-cluster-version": {
		nonRunlevel: true,
		perVersion: map[string]*versionProgress{
			v418: {done: false},
			v417: {done: false},
			v416: {
				done: false,
				prs:  []string{"https://github.com/openshift/cluster-version-operator/pull/1038"},
			},
			v415: {
				done: true,
				prs:  []string{"https://github.com/openshift/cluster-version-operator/pull/1068"},
			},
		},
	},
	"openshift-kube-scheduler-operator": {
		runlevel: true,
	},
	"openshift-kube-controller-manager": {
		runlevel: true,
	},
	"openshift-e2e-loki": {
		nonRunlevel: true,
		perVersion: map[string]*versionProgress{
			v418: {
				done: true,
				prs:  []string{"https://github.com/openshift/release/pull/56579"},
			},
			v417: {
				done: true,
				prs:  []string{"https://github.com/openshift/release/pull/56579"},
			},
			v416: {
				done: true,
				prs:  []string{"https://github.com/openshift/release/pull/56579"},
			},
			v415: {
				done: true,
				prs:  []string{"https://github.com/openshift/release/pull/56579"},
			},
		},
	},
	"openshift-kni-infra": {
		nonRunlevel: true,
		perVersion: map[string]*versionProgress{
			v418: {
				done: true,
				prs:  []string{"https://github.com/openshift/machine-config-operator/pull/4504"},
			},
			v417: {
				done: true,
				prs:  []string{"https://github.com/openshift/machine-config-operator/pull/4542"},
			},
			v416: {
				done: true,
				prs:  []string{"https://github.com/openshift/machine-config-operator/pull/4539"},
			},
			v415: {
				done: true,
				prs:  []string{"https://github.com/openshift/machine-config-operator/pull/4540"},
			},
		},
	},
	"openshift-vsphere-infra": {
		nonRunlevel: true,
		perVersion: map[string]*versionProgress{
			v418: {
				done: true,
				prs:  []string{"https://github.com/openshift/machine-config-operator/pull/4504"},
			},
			v417: {
				done: true,
				prs:  []string{"https://github.com/openshift/machine-config-operator/pull/4542"},
			},
			v416: {
				done: true,
				prs:  []string{"https://github.com/openshift/machine-config-operator/pull/4539"},
			},
			v415: {
				done: true,
				prs:  []string{"https://github.com/openshift/machine-config-operator/pull/4540"},
			},
		},
	},
	"openshift-ovirt-infra": {
		nonRunlevel: true,
		noFixNeeded: true,
	},
	"openshift-openstack-infra": {
		nonRunlevel: true,
		perVersion: map[string]*versionProgress{
			v418: {
				done: true,
				prs:  []string{"https://github.com/openshift/machine-config-operator/pull/4504"},
			},
			v417: {
				done: true,
				prs:  []string{"https://github.com/openshift/machine-config-operator/pull/4504"},
			},
			v416: {
				done: true,
				prs:  []string{"https://github.com/openshift/machine-config-operator/pull/4539"},
			},
			v415: {
				done: true,
				prs:  []string{"https://github.com/openshift/machine-config-operator/pull/4540"},
			},
		},
	},
	"openshift-nutanix-infra": {
		nonRunlevel: true,
		perVersion: map[string]*versionProgress{
			v418: {
				done: true,
				prs:  []string{"https://github.com/openshift/machine-config-operator/pull/4504"},
			},
			v417: {
				done: true,
				prs:  []string{"https://github.com/openshift/machine-config-operator/pull/4504"},
			},
			v416: {
				done: true,
				prs:  []string{"https://github.com/openshift/machine-config-operator/pull/4539"},
			},
			v415: {
				done: true,
				prs:  []string{"https://github.com/openshift/machine-config-operator/pull/4540"},
			},
		},
	},
	"openshift-cloud-platform-infra": {
		nonRunlevel: true,
		noFixNeeded: true,
	},
	"openshift-apiserver": {
		nonRunlevel: true,
		noFixNeeded: true,
	},
	"openshift-metallb-system": {
		nonRunlevel: true,
		perVersion: map[string]*versionProgress{
			v418: {
				done: true,
				prs:  []string{"https://github.com/openshift/metallb-operator/pull/238"},
			},
			v417: {
				done: true,
				prs:  []string{"https://github.com/openshift/metallb-operator/pull/240"},
			},
			v416: {
				done: false,
				prs:  []string{"https://github.com/openshift/metallb-operator/pull/241"},
			},
		},
	},
	"openshift-manila-csi-driver": {
		runlevel:    false,
		nonRunlevel: true,
		perVersion: map[string]*versionProgress{
			v417: {
				done: true,
				prs:  []string{"https://github.com/openshift/csi-driver-manila-operator/pull/234"},
			},
			v416: {
				done: true,
				prs:  []string{"https://github.com/openshift/csi-driver-manila-operator/pull/235"},
			},
			v415: {
				done: true,
				prs:  []string{"https://github.com/openshift/csi-driver-manila-operator/pull/236"},
			},
		},
	},
	"openshift-kube-proxy": {
		runlevel: true,
	},
	"openshift-sriov-network-operator": {
		nonRunlevel: true,
		perVersion: map[string]*versionProgress{
			v417: {
				done: true,
				prs: []string{
					"https://github.com/k8snetworkplumbingwg/sriov-network-operator/pull/754",
					"https://github.com/openshift/sriov-network-operator/pull/995",
				},
			},
			v416: {
				done: true,
				prs:  []string{"https://github.com/openshift/sriov-network-operator/pull/999"},
			},
			v415: {
				done: true,
				prs:  []string{"https://github.com/openshift/sriov-network-operator/pull/1003"},
			},
		},
	},
	"openshift-cluster-api": {
		runlevel: true,
	},
	"openshift-sdn": {
		runlevel: true,
	},
	"openshift-host-network": {
		nonRunlevel: true,
		noFixNeeded: true,
	},
	"openshift-operators": {
		nonRunlevel: true,
		noFixNeeded: true,
	},
	"openshift-console-user-settings": {
		nonRunlevel: true,
		noFixNeeded: true,
	},
	"openshift-config": {
		runlevel:    true,
		noFixNeeded: true,
	},
	"openshift-config-managed": {
		runlevel:    true,
		noFixNeeded: true,
	},
	"openshift-infra": {
		nonRunlevel: true,
		perVersion: map[string]*versionProgress{
			v415: {done: false},
		},
	},
	"openshift": {
		nonRunlevel: true,
		noFixNeeded: true,
	},
	"openshift-node": {
		nonRunlevel: true,
		noFixNeeded: true,
	},
	"default": {
		runlevel:    true,
		noFixNeeded: true,
	},
	"kube-public": {
		nonRunlevel: true,
		noFixNeeded: true,
	},
	"kube-node-lease": {
		nonRunlevel: true,
		noFixNeeded: true,
	},
	"kube-system": {
		runlevel: true,
	},
	"oc debug node pods": {
		nonRunlevel: true,
		perVersion: map[string]*versionProgress{
			v417: {
				done: true,
				prs:  []string{"https://github.com/openshift/oc/pull/1763"},
			},
			v416: {
				done: true,
				prs:  []string{"https://github.com/openshift/oc/pull/1816"},
			},
			v415: {
				done: true,
				prs:  []string{"https://github.com/openshift/oc/pull/1818"},
			},
		},
	},
}
