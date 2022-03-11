package docker

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Container tracks information about the docker container started for tests.
type Container struct {
	ID   string
	Host string // IP:Port
}

// StartTendermintContainer starts a Tendermint container for running tests.
func StartTendermintContainer() (*Container, error) {
	dir, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("could not get current dir: %w", err)
	}

	var out, stdErrOut bytes.Buffer

	// Init Tendermint

	cmd := exec.Command(
		"docker", "run", "--rm",
		"-v", dir+"/../testdata/tendermint:/tendermint",
		"--entrypoint", "/usr/bin/tendermint",
		"tendermint/tendermint:v0.35.1",
		"init", "validator", "--key", "secp256k1")
	cmd.Stdout = &out
	cmd.Stderr = &stdErrOut

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("could not init tendermint: %w, %s", err, stdErrOut.String())
	}

	// Start a Tendermint validator

	out.Reset()
	stdErrOut.Reset()

	cmd = exec.Command(
		"docker", "run", "-d", "--rm", "-p",
		"0.0.0.0:26657:26657/tcp",
		"-v", dir+"/../testdata/tendermint:/tendermint",
		"--entrypoint", "/usr/bin/tendermint",
		"tendermint/tendermint:v0.35.1", "start",
		"--rpc.laddr", "tcp://0.0.0.0:26657",
		"--proxy-app", "tcp://host.docker.internal:26658")
	cmd.Stdout = &out
	cmd.Stderr = &stdErrOut

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("could not start tendermint: %w, %s", err, stdErrOut.String())
	}

	id := out.String()[:12]
	c := Container{
		ID: id,
	}

	fmt.Printf("Container ID: %s\n", c.ID)

	return &c, nil
}

// StopContainer stops and removes the specified container.
func StopContainer(id string) error {
	fmt.Printf("Stopping container %s ...", id)

	if err := exec.Command("docker", "stop", id).Run(); err != nil {
		return fmt.Errorf("could not stop container: %w", err)
	}
	fmt.Println("Stopped:", id)

	if err := exec.Command("docker", "rm", id, "-v").Run(); err != nil {
		return fmt.Errorf("could not remove container: %w", err)
	}
	fmt.Println("Removed:", id)

	return nil
}

func extractIPPort(id string, port string) (hostIP string, hostPort string, err error) {
	tmpl := fmt.Sprintf("[{{range $k,$v := (index .NetworkSettings.Ports \"%s/tcp\")}}{{json $v}}{{end}}]", port)

	cmd := exec.Command("docker", "inspect", "-f", tmpl, id)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", "", fmt.Errorf("could not inspect container %s: %w", id, err)
	}

	// When IPv6 is turned on with Docker.
	// Got  [{"HostIp":"0.0.0.0","HostPort":"49190"}{"HostIp":"::","HostPort":"49190"}]
	// Need [{"HostIp":"0.0.0.0","HostPort":"49190"},{"HostIp":"::","HostPort":"49190"}]
	data := strings.ReplaceAll(out.String(), "}{", "},{")

	var docs []struct {
		HostIP   string
		HostPort string
	}
	if err := json.Unmarshal([]byte(data), &docs); err != nil {
		return "", "", fmt.Errorf("could not decode json: %w", err)
	}

	for _, doc := range docs {
		if doc.HostIP != "::" {
			return doc.HostIP, doc.HostPort, nil
		}
	}

	return "", "", fmt.Errorf("could not locate ip/port")
}
