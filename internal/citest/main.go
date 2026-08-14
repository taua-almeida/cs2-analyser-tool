package main

import (
	"flag"
	"fmt"
	"io"
	"os"
)

func main() {
	profileName := flag.String("profile", string(profilePullRequest), "summary and skip policy: pull-request, release, or external")
	fixturesVerified := flag.Bool("fixtures-verified", false, "report that the external provisioning step verified every fixture checksum")
	flag.Parse()

	profile, err := parseProfile(*profileName)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	result, parseErr := readTestEvents(os.Stdin, os.Stdout)
	report := evaluate(profile, result, parseErr, *fixturesVerified)
	markdown := renderMarkdown(profile, result, report)
	fmt.Fprint(os.Stdout, markdown)

	if summaryPath := os.Getenv("GITHUB_STEP_SUMMARY"); summaryPath != "" {
		if err := appendSummary(summaryPath, markdown); err != nil {
			fmt.Fprintf(os.Stderr, "writing GitHub Actions summary: %v\n", err)
			report.problems = append(report.problems, "GitHub Actions summary could not be written")
		}
	}

	if len(report.problems) == 0 {
		return
	}
	for _, problem := range report.problems {
		fmt.Fprintf(os.Stderr, "CI test policy: %s\n", problem)
	}
	os.Exit(1)
}

func appendSummary(path, markdown string) error {
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = io.WriteString(file, markdown)
	return err
}
