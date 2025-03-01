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
	v414 = "4.14"
	v415 = "4.15"
	v416 = "4.16"
	v417 = "4.17"
	v418 = "4.18"
	v419 = "4.19"

	currentVersion = v419

	sippyFilter = `{"items":[{"columnField":"name","operatorValue":"starts with","value":"[sig-auth] all workloads in ns/"},{"columnField":"name","operatorValue":"ends with","value":"must set the 'openshift.io/required-scc' annotation"}],"linkOperator":"and"}`
)

var (
	out = os.Stdout

	versions = []string{v414, v415, v416, v417, v418, v419}

	untestedPerVersion = map[string][]string{
		v414: {},
		v415: {},
		v416: {},
		v417: {},
		v418: {},
		v419: {},
	}

	versionStats = map[string]*stats{
		v414: {allPRs: make(map[string]struct{}), openPRs: make(map[string]struct{})},
		v415: {allPRs: make(map[string]struct{}), openPRs: make(map[string]struct{})},
		v416: {allPRs: make(map[string]struct{}), openPRs: make(map[string]struct{})},
		v417: {allPRs: make(map[string]struct{}), openPRs: make(map[string]struct{})},
		v418: {allPRs: make(map[string]struct{}), openPRs: make(map[string]struct{})},
		v419: {allPRs: make(map[string]struct{}), openPRs: make(map[string]struct{})},
	}

	ignoreNS = map[string]struct{}{
		"openshift-rukpak": {}, // see https://github.com/openshift/operator-framework-rukpak/pull/92#issuecomment-2286534684
	}

	releaseBranchesToCheck = map[string]string{
		v417: "release-4.17",
		v418: "release-4.18",
	}

	repoWithoutReleaseBranches = map[string]bool{
		"openshift/release": true,
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

				if branch := releaseBranchesToCheck[v]; len(branch) > 0 {
					// check which PRs merged after a version

					for _, pr := range ns.perVersion[v].prs {
						if prStatus(pr) == "OPEN" {
							continue
						}

						if foundInBranch, branchExistsInRepo := prInBranch(pr, branch); branchExistsInRepo && !foundInBranch {
							fmt.Println("*", v, "PR", pr, "not part of", branch, "(merged in newer release)")
						}
					}
				}
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
					baseBranch := prBaseBranch(pr)
					if v == currentVersion && baseBranch != "master" && baseBranch != "main" {
						fmt.Printf("* %s PR on wrong base branch (expected master/main): %s\n", v, pr)
					}

					if v != currentVersion {
						if baseBranch == "master" || baseBranch == "main" {
							fmt.Printf("* %s PR should be promoted to %s: %s\n", v, currentVersion, pr)
						}

						expected := fmt.Sprintf("release-%s", v)
						if baseBranch != expected {
							fmt.Printf("* %s PR on wrong version (expected %s): %s\n", v, expected, pr)
						}
					}

					vstats.openPRs[pr] = struct{}{}
					allMerged = false
				}
			}

			if len(ns.perVersion[v].prs) > 0 && allMerged {
				fmt.Printf("* all v%s PRs of %s have been merged\n", v, nsName)
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
		v414: {},
		v415: {},
		v416: {},
		v417: {},
		v418: {},
		v419: {},
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

	header := "|  #  | Component | Namespace | 4.19 Flakes | 4.18 Flakes | 4.17 Flakes | 4.16 Flakes | 4.15 Flakes | 4.14 Flakes | 4.19 PRs | 4.18 PRs | 4.17 PRs | 4.16 PRs | 4.15 PRs | 4.14 PRs |"
	subhdr := "| --- | --------- | --------- | ----------- | ----------- | ----------- | ----------- | ----------- | ----------- | -------- | -------- | -------- | -------- | -------- | -------- |"
	statsStr := getStats()

	fmt.Fprintf(out, "*Last updated: %s*\n\n", time.Now().Format(time.DateTime))
	fmt.Fprint(out, "[Authored PRs](https://github.com/pulls?q=is%3Apr+author%3Aliouk+archived%3Afalse+AUTH-482+in%3Atitle+is%3Aopen), [Assigned PRs](https://github.com/pulls?q=is%3Apr+assignee%3Aliouk+archived%3Afalse+AUTH-482+in%3Atitle+is%3Aopen), [All open PRs](https://github.com/search?q=org%3Aopenshift+is%3Apr+is%3Aopen+%2Fset+required-scc+for+openshift+workloads%2F+in%3Atitle&type=pullrequests)\n\n[Jira issue](https://issues.redhat.com/browse/AUTH-482) [Jira Backport Dashboard](https://issues.redhat.com/secure/Dashboard.jspa?selectPageId=12363204)\n\n")

	fmt.Fprintf(out, "| Tests | 4.19 | 4.18 | 4.17 | 4.16 | 4.15 | 4.14 |\n")
	fmt.Fprintf(out, "| ----- | ---- | ---- | ---- | ---- | ---- | ---- |\n")
	fmt.Fprintf(out, "| total runs | %d | %d | %d | %d | %d | %d |\n",
		versionTestStats[v419].total,
		versionTestStats[v418].total,
		versionTestStats[v417].total,
		versionTestStats[v416].total,
		versionTestStats[v415].total,
		versionTestStats[v414].total,
	)
	fmt.Fprintf(out, "| successes | %d | %d | %d | %d | %d | %d |\n",
		versionTestStats[v419].successes,
		versionTestStats[v418].successes,
		versionTestStats[v417].successes,
		versionTestStats[v416].successes,
		versionTestStats[v415].successes,
		versionTestStats[v414].successes,
	)
	fmt.Fprintf(out, "| flakes | %d | %d | %d | %d | %d | %d |\n",
		versionTestStats[v419].flakes,
		versionTestStats[v418].flakes,
		versionTestStats[v417].flakes,
		versionTestStats[v416].flakes,
		versionTestStats[v415].flakes,
		versionTestStats[v414].flakes,
	)
	fmt.Fprintf(out, "| failures | %d | %d | %d | %d | %d | %d |\n\n",
		versionTestStats[v419].failures,
		versionTestStats[v418].failures,
		versionTestStats[v417].failures,
		versionTestStats[v416].failures,
		versionTestStats[v415].failures,
		versionTestStats[v414].failures,
	)

	fmt.Fprintf(out, "| Num PRs | 4.19 | 4.18 | 4.17 | 4.16 | 4.15 | 4.14 |\n")
	fmt.Fprintf(out, "| ------- | ---- | ---- | ---- | ---- | ---- | ---- |\n")
	fmt.Fprintf(out, "| open PRs | %d | %d | %d | %d | %d | %d |\n", len(versionStats[v419].openPRs), len(versionStats[v418].openPRs), len(versionStats[v417].openPRs), len(versionStats[v416].openPRs), len(versionStats[v415].openPRs), len(versionStats[v414].openPRs))
	fmt.Fprintf(out, "| total PRs | %d | %d | %d | %d | %d | %d |\n\n", len(versionStats[v419].allPRs), len(versionStats[v418].allPRs), len(versionStats[v417].allPRs), len(versionStats[v416].allPRs), len(versionStats[v415].allPRs), len(versionStats[v414].allPRs))

	fmt.Fprintf(out, "| # namespaces | 4.19 | 4.18 | 4.17 | 4.16 | 4.15 | 4.14 |\n")
	fmt.Fprintf(out, "| ------ | ---- | ---- | ---- | ---- | ---- | ---- |\n")
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

		fmt.Fprintf(out, "| %d | %s | %s | %d | %d | %d | %d | %d | %d | %s | %s | %s | %s | %s | %s |\n",
			i+1,
			ns.jiraComponent,
			ns.nsName,
			flakes[v419],
			flakes[v418],
			flakes[v417],
			flakes[v416],
			flakes[v415],
			flakes[v414],
			prLine[v419],
			prLine[v418],
			prLine[v417],
			prLine[v416],
			prLine[v415],
			prLine[v414],
		)
	}
}

func prBaseBranch(url string) string {
	matches := regexp.MustCompile(`https\:\/\/github\.com\/(.*)\/pull\/(\d+)`).FindStringSubmatch(url)
	if len(matches) != 3 {
		panic("bad PR url format: " + url)
	}
	prNum := matches[2]
	repo := matches[1]

	var out, stderr strings.Builder
	cmd := exec.Command("gh", "pr", "view", prNum, "--repo", repo, "--json", "baseRefName", "--jq", ".baseRefName")
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		panic(fmt.Errorf("cmd '%s' failed with error: %s", cmd.String(), stderr.String()))
	}

	return strings.TrimSpace(out.String())
}

func prStatus(url string) string {
	matches := regexp.MustCompile(`https\:\/\/github\.com\/(.*)\/pull\/(\d+)`).FindStringSubmatch(url)
	if len(matches) != 3 {
		panic("bad PR url format: " + url)
	}

	var out, stderr strings.Builder
	cmd := exec.Command("gh", "pr", "view", matches[2], "--repo", matches[1], "--json", "state", "--jq", ".state")
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

	statsBuf.WriteString(fmt.Sprintf("| monitored | %d | %d | %d | %d | %d | %d |\n",
		versionStats[v419].numNS,
		versionStats[v418].numNS,
		versionStats[v417].numNS,
		versionStats[v416].numNS,
		versionStats[v415].numNS,
		versionStats[v414].numNS,
	))
	statsBuf.WriteString(fmt.Sprintf("| fix needed | %d | %d | %d | %d | %d | %d |\n",
		versionStats[v414].numNS-versionStats[v419].numNoFixNeededNS,
		versionStats[v418].numNS-versionStats[v418].numNoFixNeededNS,
		versionStats[v417].numNS-versionStats[v417].numNoFixNeededNS,
		versionStats[v416].numNS-versionStats[v416].numNoFixNeededNS,
		versionStats[v415].numNS-versionStats[v415].numNoFixNeededNS,
		versionStats[v414].numNS-versionStats[v414].numNoFixNeededNS,
	))
	statsBuf.WriteString(fmt.Sprintf("| fixed | %d | %d | %d | %d | %d | %d |\n",
		versionStats[v419].numDoneNS, versionStats[v418].numDoneNS, versionStats[v417].numDoneNS, versionStats[v416].numDoneNS, versionStats[v415].numDoneNS, versionStats[v414].numDoneNS,
	))
	statsBuf.WriteString(fmt.Sprintf("| remaining | %d | %d | %d | %d | %d | %d |\n",
		versionStats[v419].numNS-(versionStats[v419].numDoneNS+versionStats[v419].numNoFixNeededNS),
		versionStats[v418].numNS-(versionStats[v418].numDoneNS+versionStats[v418].numNoFixNeededNS),
		versionStats[v417].numNS-(versionStats[v417].numDoneNS+versionStats[v417].numNoFixNeededNS),
		versionStats[v416].numNS-(versionStats[v416].numDoneNS+versionStats[v416].numNoFixNeededNS),
		versionStats[v415].numNS-(versionStats[v415].numDoneNS+versionStats[v415].numNoFixNeededNS),
		versionStats[v414].numNS-(versionStats[v414].numDoneNS+versionStats[v414].numNoFixNeededNS),
	))
	statsBuf.WriteString(fmt.Sprintf("| ~ remaining non-runlevel | %d | %d | %d | %d | %d | %d |\n",
		// untested namespaces end up being counted as remaining-non-runlevel
		versionStats[v419].numRemainingNonRunlevelNS,
		versionStats[v418].numRemainingNonRunlevelNS,
		versionStats[v417].numRemainingNonRunlevelNS,
		versionStats[v416].numRemainingNonRunlevelNS,
		versionStats[v415].numRemainingNonRunlevelNS,
		versionStats[v414].numRemainingNonRunlevelNS,
	))
	statsBuf.WriteString(fmt.Sprintf("| ~ remaining runlevel (low-prio) | %d | %d | %d | %d | %d | %d |\n",
		versionStats[v419].numRemainingRunlevelNS,
		versionStats[v418].numRemainingRunlevelNS,
		versionStats[v417].numRemainingRunlevelNS,
		versionStats[v416].numRemainingRunlevelNS,
		versionStats[v415].numRemainingRunlevelNS,
		versionStats[v414].numRemainingRunlevelNS,
	))
	statsBuf.WriteString(fmt.Sprintf("| ~ untested | %d | %d | %d | %d | %d | %d |\n",
		len(untestedPerVersion[v419]),
		len(untestedPerVersion[v418]),
		len(untestedPerVersion[v417]),
		len(untestedPerVersion[v416]),
		len(untestedPerVersion[v415]),
		len(untestedPerVersion[v414]),
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
	jiraBlob.WriteString("|| \\# namespaces || 4.19 || 4.18 || 4.17 || 4.16 || 4.15 || 4.14 ||\n")
	jiraBlob.WriteString(fmt.Sprintf("%s\n", statsStr))
	jiraBlob.WriteString("h3. Progress breakdown\n")
	jiraBlob.WriteString("||#||namespace||4.19||4.18||4.17||4.16||4.15||4.14||\n")

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
			v419: "",
			v418: "",
			v417: "",
			v416: "",
			v415: "",
			v414: "",
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

		jiraBlob.WriteString(fmt.Sprintf("| %d | %s | %s | %s | %s | %s | %s | %s |\n",
			i,
			nsName,
			prLine[v419],
			prLine[v418],
			prLine[v417],
			prLine[v416],
			prLine[v415],
			prLine[v414],
		))
		i++
	}

	return jiraBlob.String()
}

func prName(url string) string {
	parts := strings.Split(url, "/")
	return fmt.Sprintf("#%s", parts[len(parts)-1])
}

type ghCmdError struct {
	Message string `json:"message"`
	Status  string `json:"status"`
	DocURL  string `json:"documentation_url"`
}

func prInBranch(pr, branch string) (mergedInBranch bool, branchExistsInRepo bool) {
	r := regexp.MustCompile(`https?://github\.com/([^/]+)/([^/]+)/pull/(\d+)`)
	matches := r.FindStringSubmatch(pr)

	if len(matches) != 4 {
		panic(fmt.Errorf("couldn't parse PR URL: %s", pr))
	}

	owner := matches[1]
	repo := matches[2]
	prNum := matches[3]

	if repoWithoutReleaseBranches[fmt.Sprintf("%s/%s", owner, repo)] {
		return false, false
	}

	// get PR's merge commit SHA
	var cmd *exec.Cmd
	var stdout, stderr strings.Builder
	cmd = exec.Command("gh", "pr", "view", prNum, "--repo", fmt.Sprintf("%s/%s", owner, repo), "--json", "mergeCommit", "--jq", ".mergeCommit.oid")
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		panic(fmt.Errorf("cmd '%s' failed with error: %s", cmd.String(), stderr.String()))
	}
	mergeCommitSHA := strings.TrimSpace(stdout.String())

	// check if merge commit SHA belongs to the specified branch
	stdout.Reset()
	stderr.Reset()
	cmpString := fmt.Sprintf("repos/%s/%s/compare/%s...%s", owner, repo, branch, mergeCommitSHA)
	cmd = exec.Command("gh", "api", cmpString, "--jq", ".status")
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		var cmdErr ghCmdError
		jsonErr := json.Unmarshal([]byte(stdout.String()), &cmdErr)
		if jsonErr != nil {
			panic(err)
		}

		if cmdErr.Status == "404" {
			fmt.Printf("* %s/%s commit '%s' of PR %s not found on branch %s\n", owner, repo, mergeCommitSHA, pr, branch)
			return false, true
		}

		panic(fmt.Errorf("cmd '%s' failed with error: %s", cmd.String(), stderr.String()))
	}

	cmpStatus := strings.TrimSpace(stdout.String())
	return strings.EqualFold(cmpStatus, "identical") || strings.EqualFold(cmpStatus, "behind"), true
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
			v414: {done: false},
		},
	},
	"openshift-etcd-operator": {
		runlevel: true,
	},
	"openshift-insights": {
		nonRunlevel: true,
		perVersion: map[string]*versionProgress{
			v419: {
				done: true,
				prs:  []string{"https://github.com/openshift/insights-operator/pull/1033"},
			},
			v418: {
				done: true,
				prs:  []string{"https://github.com/openshift/insights-operator/pull/1041"},
			},
			v417: {
				done: true,
				prs:  []string{"https://github.com/openshift/insights-operator/pull/1049"},
			},
			v416: {
				done: true,
				prs:  []string{"https://github.com/openshift/insights-operator/pull/915"},
			},
			v415: {
				done: true,
				prs:  []string{"https://github.com/openshift/insights-operator/pull/967"},
			},
			v414: {done: false},
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
			v414: {done: false},
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
			v414: {done: false},
		},
	},
	"openshift-monitoring": {
		nonRunlevel: true,
		perVersion: map[string]*versionProgress{
			v419: {
				done: false,
				prs: []string{
					"https://github.com/openshift/managed-cluster-config/pull/2298",
					"https://github.com/openshift/configure-alertmanager-operator/pull/366",
				},
			},
			v418: {
				done: false,
				prs:  []string{"https://github.com/openshift/cluster-monitoring-operator/pull/2498"},
			},
			v417: {
				done: false,
			},
			v416: {
				done: false,
				prs:  []string{"https://github.com/openshift/cluster-monitoring-operator/pull/2335"},
			},
			v415: {
				done: true,
				prs:  []string{"https://github.com/openshift/cluster-monitoring-operator/pull/2420"},
			},
			v414: {done: false},
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
			v414: {done: false},
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
			v414: {wontdoReason: "NoWorkload"},
		},
	},
	"openshift-ingress": {
		nonRunlevel: true,
		perVersion: map[string]*versionProgress{
			v419: {done: false},
			v418: {
				done: false,
				prs:  []string{"https://github.com/openshift/cluster-ingress-operator/pull/1032"},
			},
			v417: {done: false},
			v416: {done: false},
			v415: {done: false},
			v414: {done: false},
		},
	},
	"openshift-kube-apiserver": {
		runlevel: true,
	},
	"openshift-marketplace": {
		// we won't fix further workloads in this one: see https://github.com/openshift/origin/blob/master/test/extended/util/managed_services.go#L8-L28
		nonRunlevel: true,
		perVersion: map[string]*versionProgress{
			v418: {
				done: true,
				prs:  []string{"https://github.com/operator-framework/operator-marketplace/pull/578"},
			},
			v417: {done: true},
			v416: {
				done: true,
				prs:  []string{"https://github.com/operator-framework/operator-marketplace/pull/561"},
			},
			v415: {
				done: true,
				prs:  []string{"https://github.com/operator-framework/operator-marketplace/pull/570"},
			},
			v414: {done: true},
		},
	},
	"openshift-network-console": {
		nonRunlevel: true,
		perVersion: map[string]*versionProgress{
			v418: {
				done: true,
				prs:  []string{"https://github.com/openshift/cluster-network-operator/pull/2545"},
			},
			v417: {done: false},
			v416: {done: false},
			v415: {done: false},
			v414: {done: false},
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
			v415: {done: false},
			v414: {done: false},
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
			v414: {done: false},
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
			v414: {done: false},
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
			v414: {done: false},
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
			v414: {done: false},
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
			v414: {done: false},
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
			v414: {done: false},
		},
	},
	"openshift-machine-config-operator": {
		nonRunlevel: true,
		perVersion: map[string]*versionProgress{
			v418: {
				done: true,
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
			v414: {done: false},
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
			v415: {done: false},
			v414: {done: false},
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
			v414: {done: false},
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
			v414: {done: false},
		},
	},
	"openshift-image-registry": {
		nonRunlevel: true,
		perVersion: map[string]*versionProgress{
			v419: {done: false},
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
			v414: {done: false},
		},
	},
	"openshift-ingress-canary": {
		nonRunlevel: true,
		perVersion: map[string]*versionProgress{
			v419: {done: false},
			v418: {
				done: false,
				prs:  []string{"https://github.com/openshift/cluster-ingress-operator/pull/1031"},
			},
			v417: {done: false},
			v416: {done: false},
			v415: {done: false},
			v414: {done: false},
		},
	},
	"openshift-ingress-operator": {
		nonRunlevel: true,
		perVersion: map[string]*versionProgress{
			v419: {done: false},
			v418: {
				done: false,
				prs:  []string{"https://github.com/openshift/cluster-ingress-operator/pull/1031"},
			},
			v417: {done: false},
			v416: {done: false},
			v415: {done: false},
			v414: {done: false},
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
			v414: {done: false},
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
			v414: {done: false},
		},
	},
	"openshift-storage": {
		runlevel: true,
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
			v414: {done: false},
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
			v415: {done: false},
			v414: {done: false},
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
			v414: {done: false},
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
			v414: {done: false},
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
			v414: {done: false},
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
			v414: {done: false},
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
			v419: {
				done: false,
				prs: []string{
					"https://github.com/openshift/csi-driver-shared-resource-operator/pull/118",
					"https://github.com/openshift/hypershift/pull/5310",
					"https://github.com/openshift/ibm-vpc-block-csi-driver-operator/pull/135",
				},
			},
			v418: {
				done: false,
				prs: []string{
					"https://github.com/openshift/cluster-storage-operator/pull/524",
					"https://github.com/openshift/gcp-pd-csi-driver-operator/pull/131",
					"https://github.com/openshift/csi-operator/pull/306",
					"https://github.com/openshift/vmware-vsphere-csi-driver-operator/pull/265",
					"https://github.com/openshift/ibm-powervs-block-csi-driver-operator/pull/75",
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
			v414: {done: false},
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
			v414: {done: false},
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
			v414: {done: false},
		},
	},
	"openshift-machine-api": {
		nonRunlevel: true,
		perVersion: map[string]*versionProgress{
			v419: {
				done: false,
				prs: []string{
					"https://github.com/openshift/machine-api-operator/pull/1308",
					"https://github.com/openshift/machine-api-operator/pull/1317",
				},
			},
			v418: {
				done: false,
				prs:  []string{"https://github.com/openshift/machine-api-operator/pull/1311"},
			},
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
			v414: {done: false},
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
			v414: {done: false},
		},
	},
	"openshift-kube-scheduler": {
		runlevel: true,
	},
	"openshift-cluster-version": {
		nonRunlevel: true,
		perVersion: map[string]*versionProgress{
			v419: {done: false},
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
			v414: {done: false},
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
			v414: {done: false},
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
			v414: {done: false},
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
			v414: {done: false},
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
				done: false,
				prs:  []string{},
			},
			v416: {
				done: true,
				prs:  []string{"https://github.com/openshift/machine-config-operator/pull/4539"},
			},
			v415: {
				done: true,
				prs:  []string{"https://github.com/openshift/machine-config-operator/pull/4540"},
			},
			v414: {done: false},
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
				prs:  []string{},
			},
			v416: {
				done: true,
				prs:  []string{"https://github.com/openshift/machine-config-operator/pull/4539"},
			},
			v415: {
				done: true,
				prs:  []string{"https://github.com/openshift/machine-config-operator/pull/4540"},
			},
			v414: {done: false},
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
			v415: {done: false},
			v414: {done: false},
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
			v414: {done: false},
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
			v414: {done: false},
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
		noFixNeeded: true,
		perVersion: map[string]*versionProgress{
			v415: {done: false},
			v414: {done: false},
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
			v414: {done: false},
		},
	},
}
