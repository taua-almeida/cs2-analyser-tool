package main

import (
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"
)

func TestReadTestEventsParsesActionsAndPreservesOutput(t *testing.T) {
	input := strings.NewReader(strings.Join([]string{
		`{"Action":"output","Package":"github.com/taua-almeida/cs2-analyser-tool/analysis","Test":"TestFails","Output":"failure detail\n"}`,
		`{"Action":"pass","Package":"github.com/taua-almeida/cs2-analyser-tool/analysis","Test":"TestPasses"}`,
		`{"Action":"fail","Package":"github.com/taua-almeida/cs2-analyser-tool/analysis","Test":"TestFails"}`,
		`{"Action":"skip","Package":"github.com/taua-almeida/cs2-analyser-tool/cmd/dataexport","Test":"TestSkipped"}`,
		`{"Action":"fail","Package":"github.com/taua-almeida/cs2-analyser-tool/analysis"}`,
	}, "\n"))

	var output strings.Builder
	run, err := readTestEvents(input, &output)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := output.String(), "failure detail\n"; got != want {
		t.Fatalf("readable output = %q, want %q", got, want)
	}
	if !run.underlyingFailed {
		t.Fatal("package failure was not recorded")
	}
	if got, want := run.counts(), (testCounts{passed: 1, failed: 1, skipped: 1}); got != want {
		t.Fatalf("counts = %+v, want %+v", got, want)
	}
	wantActions := map[string]string{
		"analysis/TestPasses":        "pass",
		"analysis/TestFails":         "fail",
		"cmd/dataexport/TestSkipped": "skip",
	}
	if !reflect.DeepEqual(run.actions, wantActions) {
		t.Fatalf("actions = %#v, want %#v", run.actions, wantActions)
	}
}

func TestUnexplainedPackageFailureCountsAsOneFailure(t *testing.T) {
	input := strings.NewReader(strings.Join([]string{
		`{"Action":"pass","Package":"github.com/taua-almeida/cs2-analyser-tool/analysis","Test":"TestPasses"}`,
		`{"Action":"output","Package":"github.com/taua-almeida/cs2-analyser-tool/cmd/dataexport","Output":"exit status 1\n"}`,
		`{"Action":"fail","Package":"github.com/taua-almeida/cs2-analyser-tool/cmd/dataexport"}`,
	}, "\n"))

	run, err := readTestEvents(input, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if !run.underlyingFailed {
		t.Fatal("package failure was not recorded")
	}
	if got, want := run.unexplainedPackageFailures(), []string{"cmd/dataexport"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unexplained package failures = %#v, want %#v", got, want)
	}
	if got, want := run.counts(), (testCounts{passed: 1, failed: 1}); got != want {
		t.Fatalf("counts = %+v, want %+v", got, want)
	}

	report := evaluate(profilePullRequest, run, nil, false)
	if markdown := renderMarkdown(profilePullRequest, run, report); !strings.Contains(markdown, "- Failed: 1\n") {
		t.Fatalf("summary does not count the package-level failure:\n%s", markdown)
	}
}

func TestPackageFailureExplainedByFailedTestIsNotDoubleCounted(t *testing.T) {
	input := strings.NewReader(strings.Join([]string{
		`{"Action":"fail","Package":"github.com/taua-almeida/cs2-analyser-tool/analysis","Test":"TestFails"}`,
		`{"Action":"fail","Package":"github.com/taua-almeida/cs2-analyser-tool/analysis"}`,
		`{"Action":"fail","Package":"github.com/taua-almeida/cs2-analyser-tool/cmd/dataexport"}`,
	}, "\n"))

	run, err := readTestEvents(input, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := run.unexplainedPackageFailures(), []string{"cmd/dataexport"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unexplained package failures = %#v, want %#v", got, want)
	}
	if got, want := run.counts(), (testCounts{failed: 2}); got != want {
		t.Fatalf("counts = %+v, want %+v", got, want)
	}
}

func TestPullRequestAllowlistAcceptsEveryExactSkip(t *testing.T) {
	rules, err := compileSkipRules(pullRequestSkipEntries)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"analysis/TestAnalyseGolden/inferno_shotgun_halftime_and_freeze_join",
		"analysis/TestHLTVSeriesRegression",
		"analysis/TestEvaluateHLTVTradeModels",
		"analysis/TestTraceHLTVRoundEvidence",
	} {
		if !allowedBy(rules, name) {
			t.Errorf("exact skip %s was rejected", name)
		}
	}
}

func TestHLTVMapPrefixAcceptsOnlyRegressionSubtests(t *testing.T) {
	rules, err := compileSkipRules([]string{"analysis/TestHLTVRegression/*"})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"analysis/TestHLTVRegression/inferno",
		"analysis/TestHLTVRegression/extra/match-128974/dust2-234227",
	} {
		if !allowedBy(rules, name) {
			t.Errorf("intended map subtest %s was rejected", name)
		}
	}
	for _, name := range []string{
		"analysis/TestHLTVRegression",
		"analysis/TestHLTVRegressionNew/inferno",
		"analysis/TestHLTVSeriesRegression/inferno",
		"cmd/TestHLTVRegression/inferno",
	} {
		if allowedBy(rules, name) {
			t.Errorf("unrelated test %s matched the map prefix", name)
		}
	}
}

func TestCompileSkipRulesRejectsBroadOrMalformedRules(t *testing.T) {
	for _, entry := range []string{
		"analysis/*",
		"analysis/TestHLTV*",
		"analysis/TestHLTVRegression/**",
		"analysis/TestHLTVRegression/",
		"*/TestHLTVRegression",
		"analysis//TestHLTVRegression",
		" analysis/TestHLTVRegression",
	} {
		t.Run(entry, func(t *testing.T) {
			if _, err := compileSkipRules([]string{entry}); err == nil {
				t.Fatalf("compileSkipRules(%q) succeeded", entry)
			}
		})
	}
}

func TestPullRequestPolicyRejectsUnexpectedSkip(t *testing.T) {
	run := passingPublicGoldenRun()
	run.actions["analysis/TestNewConditionalPath"] = "skip"

	report := evaluate(profilePullRequest, run, nil, false)
	if got, want := report.unexpectedSkips, []string{"analysis/TestNewConditionalPath"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected skips = %#v, want %#v", got, want)
	}
	if !containsProblem(report.problems, "unexpected skipped test analysis/TestNewConditionalPath") {
		t.Fatalf("problems = %#v, want unexpected-skip failure", report.problems)
	}
}

func TestPolicyFailsWhenUnderlyingTestRunFailed(t *testing.T) {
	run := passingPublicGoldenRun()
	run.underlyingFailed = true

	report := evaluate(profilePullRequest, run, nil, false)
	if !containsProblem(report.problems, "go test reported a failed result") {
		t.Fatalf("problems = %#v, want underlying go test failure", report.problems)
	}
}

func TestPullRequestMarkdownIsDeterministic(t *testing.T) {
	run := passingPublicGoldenRun()
	run.actions["analysis/TestTraceHLTVRoundEvidence"] = "skip"
	run.actions[privateGoldenTest] = "skip"
	report := evaluate(profilePullRequest, run, nil, false)

	want := `## Go test coverage

- Passed: 2
- Failed: 0
- Skipped: 2
- Public golden fixtures: ran
- Private Inferno golden: intentionally unavailable
- HLTV map regression: external workflow
- HLTV series regression: external workflow
- Trade-model diagnostic: manual
- Trace diagnostic: manual

### Allowed skipped tests

- ` + "`analysis/TestAnalyseGolden/inferno_shotgun_halftime_and_freeze_join`" + `
- ` + "`analysis/TestTraceHLTVRoundEvidence`" + `

`
	for range 20 {
		if got := renderMarkdown(profilePullRequest, run, report); got != want {
			t.Fatalf("markdown mismatch:\n--- got ---\n%s--- want ---\n%s", got, want)
		}
	}
}

func TestExternalPolicyRequiresEightMapsSeriesAndNoSkips(t *testing.T) {
	run := testRun{actions: map[string]string{hltvMapRegression: "pass", hltvSeriesRegression: "pass"}}
	for i := 1; i <= externalMapCount; i++ {
		run.actions[fmt.Sprintf("%smap-%d", hltvMapTestPrefix, i)] = "pass"
	}

	report := evaluate(profileExternal, run, nil, true)
	if len(report.problems) != 0 {
		t.Fatalf("problems = %#v, want success", report.problems)
	}
	if got := renderMarkdown(profileExternal, run, report); !strings.Contains(got, "- Final test result: passed") {
		t.Fatalf("external summary did not report success:\n%s", got)
	}

	run.actions[hltvMapTestPrefix+"a"] = "skip"
	report = evaluate(profileExternal, run, nil, true)
	if !containsProblem(report.problems, "required external test skipped") {
		t.Fatalf("problems = %#v, want required-skip failure", report.problems)
	}
	if got := renderMarkdown(profileExternal, run, report); !strings.Contains(got, "- Final test result: failed") {
		t.Fatalf("external summary did not report failure:\n%s", got)
	}
}

func TestExternalMarkdownSeparatesRunsFailuresAndVerifiedFixtures(t *testing.T) {
	run := testRun{actions: map[string]string{hltvMapRegression: "fail", hltvSeriesRegression: "fail"}, underlyingFailed: true}
	for i := 1; i <= externalMapCount; i++ {
		action := "pass"
		if i == externalMapCount {
			action = "fail"
		}
		run.actions[fmt.Sprintf("%smap-%d", hltvMapTestPrefix, i)] = action
	}

	report := evaluate(profileExternal, run, nil, true)
	markdown := renderMarkdown(profileExternal, run, report)
	for _, want := range []string{
		"- Eight map regressions ran: yes (8/8 completed, 7 passed)",
		"- Series regression ran: yes (fail)",
		"- Fixture checksums passed: yes",
		"- Final test result: failed",
	} {
		if !strings.Contains(markdown, want) {
			t.Fatalf("external summary does not contain %q:\n%s", want, markdown)
		}
	}
}

func passingPublicGoldenRun() testRun {
	actions := make(map[string]string, len(publicGoldenTests))
	for _, name := range publicGoldenTests {
		actions[name] = "pass"
	}
	return testRun{actions: actions}
}

func containsProblem(problems []string, part string) bool {
	for _, problem := range problems {
		if strings.Contains(problem, part) {
			return true
		}
	}
	return false
}
