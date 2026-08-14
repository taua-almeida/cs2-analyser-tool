package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
)

const modulePath = "github.com/taua-almeida/cs2-analyser-tool"

type profile string

const (
	profilePullRequest profile = "pull-request"
	profileNightly     profile = "nightly"
	profileExternal    profile = "external"
)

var pullRequestSkipEntries = []string{
	"analysis/TestAnalyseGolden/inferno_shotgun_halftime_and_freeze_join",
	"analysis/TestHLTVRegression/*",
	"analysis/TestHLTVSeriesRegression",
	"analysis/TestEvaluateHLTVTradeModels",
	"analysis/TestTraceHLTVRoundEvidence",
}

var publicGoldenTests = []string{
	"analysis/TestAnalyseGolden/ancient_scoreboard_mvps",
	"analysis/TestAnalyseGolden/mirage_round_mvp_events",
}

const (
	privateGoldenTest    = "analysis/TestAnalyseGolden/inferno_shotgun_halftime_and_freeze_join"
	hltvMapRegression    = "analysis/TestHLTVRegression"
	hltvMapTestPrefix    = "analysis/TestHLTVRegression/"
	hltvSeriesRegression = "analysis/TestHLTVSeriesRegression"
	externalMapCount     = 8
)

type testEvent struct {
	Action  string
	Package string
	Test    string
	Output  string
}

type testCounts struct {
	passed  int
	failed  int
	skipped int
}

type testRun struct {
	actions                 map[string]string
	packageFailures         map[string]bool
	packagesWithFailedTests map[string]bool
	underlyingFailed        bool
}

func readTestEvents(input io.Reader, readableOutput io.Writer) (testRun, error) {
	run := testRun{
		actions:                 make(map[string]string),
		packageFailures:         make(map[string]bool),
		packagesWithFailedTests: make(map[string]bool),
	}
	decoder := json.NewDecoder(input)
	for {
		var event testEvent
		err := decoder.Decode(&event)
		if errors.Is(err, io.EOF) {
			return run, nil
		}
		if err != nil {
			return run, fmt.Errorf("decoding go test JSON: %w", err)
		}
		if event.Output != "" {
			if _, err := io.WriteString(readableOutput, event.Output); err != nil {
				return run, fmt.Errorf("writing readable test output: %w", err)
			}
		}
		if event.Action == "fail" {
			run.underlyingFailed = true
			if event.Test == "" {
				run.packageFailures[event.Package] = true
			} else {
				run.packagesWithFailedTests[event.Package] = true
			}
		}
		if event.Test == "" || !isFinalTestAction(event.Action) {
			continue
		}
		run.actions[testKey(event.Package, event.Test)] = event.Action
	}
}

func isFinalTestAction(action string) bool {
	return action == "pass" || action == "fail" || action == "skip"
}

func testKey(packagePath, testName string) string {
	return packageLabel(packagePath) + "/" + testName
}

func packageLabel(packagePath string) string {
	if packagePath == modulePath {
		return "."
	}
	if relative, ok := strings.CutPrefix(packagePath, modulePath+"/"); ok {
		return relative
	}
	return packagePath
}

func (run testRun) counts() testCounts {
	var counts testCounts
	for _, action := range run.actions {
		switch action {
		case "pass":
			counts.passed++
		case "fail":
			counts.failed++
		case "skip":
			counts.skipped++
		}
	}
	counts.failed += len(run.unexplainedPackageFailures())
	return counts
}

// unexplainedPackageFailures lists packages that failed without any failed
// test naming the cause: a TestMain os.Exit, an init panic, or a build failure
// emits only a package-level fail event with an empty Test field.
func (run testRun) unexplainedPackageFailures() []string {
	var labels []string
	for packagePath := range run.packageFailures {
		if run.packagesWithFailedTests[packagePath] {
			continue
		}
		labels = append(labels, packageLabel(packagePath))
	}
	sort.Strings(labels)
	return labels
}

func (run testRun) testsWithAction(action string) []string {
	var names []string
	for name, got := range run.actions {
		if got == action {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

type skipRule struct {
	exact  string
	prefix string
}

func compileSkipRules(entries []string) ([]skipRule, error) {
	rules := make([]skipRule, 0, len(entries))
	seen := make(map[string]bool, len(entries))
	for _, entry := range entries {
		if entry == "" || strings.TrimSpace(entry) != entry || strings.ContainsAny(entry, "\t\r\n ") {
			return nil, fmt.Errorf("invalid skip rule %q: whitespace and empty rules are not allowed", entry)
		}
		if seen[entry] {
			return nil, fmt.Errorf("invalid skip rule %q: duplicate rule", entry)
		}
		seen[entry] = true

		testSeparator := strings.Index(entry, "/Test")
		if testSeparator <= 0 {
			return nil, fmt.Errorf("invalid skip rule %q: expected package/TestName", entry)
		}
		packageName := entry[:testSeparator]
		testPattern := entry[testSeparator+1:]
		if strings.HasPrefix(packageName, "/") || strings.HasSuffix(packageName, "/") || strings.Contains(packageName, "//") || strings.Contains(packageName, "*") {
			return nil, fmt.Errorf("invalid skip rule %q: package must be exact", entry)
		}

		rule := skipRule{}
		switch strings.Count(testPattern, "*") {
		case 0:
			if testPattern == "Test" || strings.HasSuffix(testPattern, "/") || strings.Contains(testPattern, "//") {
				return nil, fmt.Errorf("invalid skip rule %q: incomplete test name", entry)
			}
			rule.exact = entry
		case 1:
			if !strings.HasSuffix(testPattern, "/*") {
				return nil, fmt.Errorf("invalid skip rule %q: only a trailing subtest /* is allowed", entry)
			}
			topLevelTest := strings.TrimSuffix(testPattern, "/*")
			if topLevelTest == "Test" || strings.Contains(topLevelTest, "/") {
				return nil, fmt.Errorf("invalid skip rule %q: prefix must name one top-level test", entry)
			}
			rule.prefix = strings.TrimSuffix(entry, "*")
		default:
			return nil, fmt.Errorf("invalid skip rule %q: only one trailing wildcard is allowed", entry)
		}
		rules = append(rules, rule)
	}
	return rules, nil
}

func (rule skipRule) matches(testName string) bool {
	if rule.exact != "" {
		return testName == rule.exact
	}
	return strings.HasPrefix(testName, rule.prefix) && len(testName) > len(rule.prefix)
}

type policyReport struct {
	allowedSkips             []string
	unexpectedSkips          []string
	problems                 []string
	mapRuns                  int
	mapPasses                int
	seriesAction             string
	fixtureChecksumsVerified bool
}

func evaluate(profile profile, run testRun, parseErr error, fixturesVerified bool) policyReport {
	report := policyReport{fixtureChecksumsVerified: fixturesVerified}
	if parseErr != nil {
		report.problems = append(report.problems, parseErr.Error())
	}
	if run.underlyingFailed {
		report.problems = append(report.problems, "go test reported a failed result")
	}

	switch profile {
	case profilePullRequest, profileNightly:
		rules, err := compileSkipRules(pullRequestSkipEntries)
		if err != nil {
			report.problems = append(report.problems, err.Error())
			break
		}
		for _, skipped := range run.testsWithAction("skip") {
			if allowedBy(rules, skipped) {
				report.allowedSkips = append(report.allowedSkips, skipped)
				continue
			}
			report.unexpectedSkips = append(report.unexpectedSkips, skipped)
			report.problems = append(report.problems, fmt.Sprintf("unexpected skipped test %s", skipped))
		}
		for _, required := range publicGoldenTests {
			if run.actions[required] != "pass" {
				report.problems = append(report.problems, fmt.Sprintf("required public golden test %s did not pass", required))
			}
		}
	case profileExternal:
		for name, action := range run.actions {
			if !strings.HasPrefix(name, hltvMapTestPrefix) {
				continue
			}
			switch action {
			case "pass":
				report.mapRuns++
				report.mapPasses++
			case "fail":
				report.mapRuns++
			}
		}
		report.seriesAction = run.actions[hltvSeriesRegression]
		if skipped := run.testsWithAction("skip"); len(skipped) > 0 {
			report.unexpectedSkips = skipped
			for _, name := range skipped {
				report.problems = append(report.problems, fmt.Sprintf("required external test skipped: %s", name))
			}
		}
		if report.mapRuns != externalMapCount {
			report.problems = append(report.problems, fmt.Sprintf("HLTV map regressions ran %d/%d subtests", report.mapRuns, externalMapCount))
		}
		if report.mapPasses != externalMapCount {
			report.problems = append(report.problems, fmt.Sprintf("HLTV map regressions passed %d/%d subtests", report.mapPasses, externalMapCount))
		}
		if run.actions[hltvMapRegression] != "pass" {
			report.problems = append(report.problems, "HLTV map regression parent did not pass")
		}
		if report.seriesAction != "pass" {
			report.problems = append(report.problems, "HLTV series regression did not pass")
		}
		if !report.fixtureChecksumsVerified {
			report.problems = append(report.problems, "external fixture checksum verification was not confirmed")
		}
	}

	return report
}

func allowedBy(rules []skipRule, testName string) bool {
	for _, rule := range rules {
		if rule.matches(testName) {
			return true
		}
	}
	return false
}

func renderMarkdown(profile profile, run testRun, report policyReport) string {
	if profile == profileExternal {
		return renderExternalMarkdown(run, report)
	}

	counts := run.counts()
	var markdown strings.Builder
	title := "Go test coverage"
	if profile == profileNightly {
		title += " (nightly)"
	}
	fmt.Fprintf(&markdown, "## %s\n\n", title)
	fmt.Fprintf(&markdown, "- Passed: %d\n", counts.passed)
	fmt.Fprintf(&markdown, "- Failed: %d\n", counts.failed)
	fmt.Fprintf(&markdown, "- Skipped: %d\n", counts.skipped)
	fmt.Fprintf(&markdown, "- Public golden fixtures: %s\n", publicGoldenStatus(run))
	if profile == profilePullRequest {
		fmt.Fprintf(&markdown, "- Private Inferno golden: %s\n", pullRequestPrivateGoldenStatus(run))
		markdown.WriteString("- HLTV map regression: external workflow\n")
		markdown.WriteString("- HLTV series regression: external workflow\n")
	} else {
		fmt.Fprintf(&markdown, "- Private Inferno golden: %s\n", nightlyPrivateGoldenStatus(run))
		markdown.WriteString("- HLTV map regression: separate required step\n")
		markdown.WriteString("- HLTV series regression: separate required step\n")
	}
	markdown.WriteString("- Trade-model diagnostic: manual\n")
	markdown.WriteString("- Trace diagnostic: manual\n")

	markdown.WriteString("\n### Allowed skipped tests\n\n")
	writeMarkdownNames(&markdown, report.allowedSkips)
	if len(report.unexpectedSkips) > 0 {
		markdown.WriteString("\n### Unexpected skipped tests\n\n")
		writeMarkdownNames(&markdown, report.unexpectedSkips)
	}
	markdown.WriteByte('\n')
	return markdown.String()
}

func renderExternalMarkdown(run testRun, report policyReport) string {
	var markdown strings.Builder
	markdown.WriteString("## External regression coverage\n\n")
	fmt.Fprintf(&markdown, "- Eight map regressions ran: %s (%d/%d completed, %d passed)\n", yesNo(report.mapRuns == externalMapCount), report.mapRuns, externalMapCount, report.mapPasses)
	fmt.Fprintf(&markdown, "- Series regression ran: %s (%s)\n", yesNo(report.seriesAction == "pass" || report.seriesAction == "fail"), testActionStatus(report.seriesAction))
	fmt.Fprintf(&markdown, "- Fixture checksums passed: %s\n", yesNo(report.fixtureChecksumsVerified))
	fmt.Fprintf(&markdown, "- No required tests skipped: %s\n", yesNo(len(report.unexpectedSkips) == 0))
	fmt.Fprintf(&markdown, "- Final test result: %s\n\n", passFail(len(report.problems) == 0 && !run.underlyingFailed))
	if len(report.unexpectedSkips) > 0 {
		markdown.WriteString("### Skipped required tests\n\n")
		writeMarkdownNames(&markdown, report.unexpectedSkips)
		markdown.WriteByte('\n')
	}
	return markdown.String()
}

func writeMarkdownNames(markdown *strings.Builder, names []string) {
	if len(names) == 0 {
		markdown.WriteString("- None\n")
		return
	}
	for _, name := range names {
		fmt.Fprintf(markdown, "- `%s`\n", strings.ReplaceAll(name, "`", "\\`"))
	}
}

func publicGoldenStatus(run testRun) string {
	for _, name := range publicGoldenTests {
		if run.actions[name] != "pass" {
			return "not confirmed"
		}
	}
	return "ran"
}

func pullRequestPrivateGoldenStatus(run testRun) string {
	switch run.actions[privateGoldenTest] {
	case "skip":
		return "intentionally unavailable"
	case "pass":
		return "ran"
	case "fail":
		return "failed"
	default:
		return "not observed"
	}
}

func nightlyPrivateGoldenStatus(run testRun) string {
	switch run.actions[privateGoldenTest] {
	case "skip":
		return "unavailable"
	case "pass":
		return "ran"
	case "fail":
		return "failed"
	default:
		return "not observed"
	}
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func passFail(passed bool) string {
	if passed {
		return "passed"
	}
	return "failed"
}

func testActionStatus(action string) string {
	if action == "" {
		return "not observed"
	}
	return action
}

func parseProfile(value string) (profile, error) {
	switch profile(value) {
	case profilePullRequest, profileNightly, profileExternal:
		return profile(value), nil
	default:
		return "", fmt.Errorf("unknown CI test profile %q", value)
	}
}
