package kit

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
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
		"-v", dir+"/"+TendermintConsensusTestDir+":/tendermint",
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
		"docker", "run", "-d", "--rm",
		"-p", "0.0.0.0:26657:26657/tcp",
		"-v", dir+"/"+TendermintConsensusTestDir+":/tendermint",
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
	fmt.Printf("Stopping and removing container %s ...\n", id)

	if err := exec.Command("docker", "stop", id).Run(); err != nil {
		return fmt.Errorf("could not stop container: %w", err)
	}
	fmt.Println("Stopped:", id)

	if err := exec.Command("docker", "rm", "--force", id, "-v").Run(); err != nil {
		return fmt.Errorf("could not remove container: %w", err)
	}
	fmt.Println("Removed:", id)

	return nil
}
