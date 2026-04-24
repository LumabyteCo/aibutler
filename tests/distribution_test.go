package tests

import (
	"os"
	"testing"
)

func TestGoreleaserYMLExists(t *testing.T) {
	if _, err := os.Stat("../.goreleaser.yml"); os.IsNotExist(err) {
		t.Fatal(".goreleaser.yml not found at repository root")
	}
}

func TestDockerfileExists(t *testing.T) {
	if _, err := os.Stat("../Dockerfile"); os.IsNotExist(err) {
		t.Fatal("Dockerfile not found at repository root")
	}
}

func TestDockerComposeExists(t *testing.T) {
	if _, err := os.Stat("../docker-compose.yml"); os.IsNotExist(err) {
		t.Fatal("docker-compose.yml not found at repository root")
	}
}

func TestHelmChartExists(t *testing.T) {
	if _, err := os.Stat("../deploy/helm/aibutler/Chart.yaml"); os.IsNotExist(err) {
		t.Fatal("deploy/helm/aibutler/Chart.yaml not found")
	}
}

func TestSystemdServiceExists(t *testing.T) {
	if _, err := os.Stat("../deploy/systemd/aibutler.service"); os.IsNotExist(err) {
		t.Fatal("deploy/systemd/aibutler.service not found")
	}
}
