package ci

import (
	"os"
	"strings"
	"testing"
)

const sharedPublisherSHA = "8e27d95846671a9e319f1900e86a488a1d4f39b3"

func TestImagePublicationWorkflowContract(t *testing.T) {
	workflow, err := os.ReadFile("../.github/workflows/lint-test-build.yml")
	if err != nil {
		t.Fatal(err)
	}
	contents := string(workflow)

	required := []string{
		"pull_request:",
		"if: github.event_name == 'pull_request'",
		"if: github.ref == 'refs/heads/main' || (github.event_name == 'push' && startsWith(github.ref, 'refs/tags/'))",
		"Build native image without credentials",
		"persist-credentials: false",
		"go mod tidy -diff",
		"libops/.github/.github/workflows/build-push.yaml@" + sharedPublisherSHA,
		"ref: ${{ github.sha }}",
		"expected-main-sha: ${{ github.ref == 'refs/heads/main' && github.sha || '' }}",
		"scan: true",
		"sign: true",
		"certificate-identity: https://github.com/libops/.github/.github/workflows/build-push.yaml@" + sharedPublisherSHA,
		"packages: write",
		"id-token: write",
	}
	for _, value := range required {
		if !strings.Contains(contents, value) {
			t.Errorf("image workflow must contain %q", value)
		}
	}

	forbidden := []string{
		"build-push.yaml@main",
		"build-push-ghcr.yaml",
		"secrets: inherit",
		"secrets.",
		"docker-registry:",
		"additional-gar-registry:",
	}
	for _, value := range forbidden {
		if strings.Contains(contents, value) {
			t.Errorf("image workflow must not contain %q", value)
		}
	}
}

func TestRuntimeImageContract(t *testing.T) {
	dockerfile, err := os.ReadFile("../Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	contents := string(dockerfile)

	for _, value := range []string{
		"FROM golang:1.25.12-alpine@sha256:",
		"FROM alpine:3.24@sha256:",
		"USER 65532:65532",
		`CMD ["/app/binary"]`,
	} {
		if !strings.Contains(contents, value) {
			t.Errorf("Dockerfile must contain %q", value)
		}
	}

	if strings.Contains(contents, "ghcr.io/libops/go") {
		t.Error("Lightsout must not depend on a moving LibOps Go utility image")
	}
}
