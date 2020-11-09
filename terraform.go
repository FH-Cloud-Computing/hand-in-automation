package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"strings"

	"github.com/containerssh/log"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

func readToLogger(input io.ReadCloser, logger log.Logger) error {
	b := make([]byte, 1)
	var buf bytes.Buffer
	for {
		n, err := input.Read(b)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return err
		}
		if n == 0 {
			break
		}
		if bytes.Equal(b, []byte("\n")) {
			line := strings.TrimSpace(buf.String())
			logger.Debugf("terraform:\t%s", line)
			buf = bytes.Buffer{}
		} else {
			buf.Write(b)
		}
	}
	if buf.Len() > 0 {
		line := strings.TrimSpace(buf.String())
		logger.Debugf("terraform:\t%s", line)
	}
	return nil
}

func executeTerraform(ctx context.Context, dockerClient *client.Client, directory string, command []string, logger log.Logger) error {
	pullResult, err := dockerClient.ImagePull(ctx, "janoszen/terraform", types.ImagePullOptions{})
	if err != nil {
		logger.Warningf("failed to pull Terraform container image (%v)", err)
		return readToLogger(pullResult, logger)
	}

	terraformContainer, err := dockerClient.ContainerCreate(
		ctx,
		&container.Config{
			Env:   nil,
			Cmd:   command,
			Image: "janoszen/terraform",
		},
		&container.HostConfig{
			Mounts: []mount.Mount{
				{
					Type:   "bind",
					Source: directory,
					Target: "/terraform",
				},
			},
		},
		&network.NetworkingConfig{},
		"",
	)
	if err != nil {
		logger.Warningf("failed to create Terraform container (%v)", err)
		return err
	}
	defer func() {
		err := dockerClient.ContainerRemove(
			ctx,
			terraformContainer.ID,
			types.ContainerRemoveOptions{
				Force: true,
			},
		)
		if err != nil {
			logger.Warningf("failed to remove container %s (%v)", terraformContainer.ID, err)
		}
	}()
	err = dockerClient.ContainerStart(ctx, terraformContainer.ID, types.ContainerStartOptions{})
	if err != nil {
		logger.Warningf("failed to start Terraform container (%v)", err)
		return err
	}
	containerOutput, err := dockerClient.ContainerLogs(ctx, terraformContainer.ID, types.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
	})
	if err != nil {
		logger.Warningf("failed to stream logs from Terraform container (%v)", err)
		return err
	}
	defer func() {
		_ = containerOutput.Close()
	}()
	logBuffer := &bytes.Buffer{}
	_, err = stdcopy.StdCopy(logBuffer, logBuffer, containerOutput)
	if err != nil {
		logger.Warningf("failed to stream logs from Terraform container (%v)", err)
		return err
	}

	if err := readToLogger(ioutil.NopCloser(logBuffer), logger); err != nil {
		return err
	}

	inspect, err := dockerClient.ContainerInspect(ctx, terraformContainer.ID)
	if err != nil {
		logger.Warningf("failed to inspect Terraform container (%v)", err)
		return err
	}
	logBuffer.Reset()

	if inspect.State.ExitCode != 0 {
		logger.Warningf("terraform %s failed", command[0])
		return fmt.Errorf("terraform %s failed", command[0])
	}

	return nil
}
