package main

import (
	"bytes"
	"context"
	"fmt"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"io"
	"log"
	"os"
)

func executeTerraform(ctx context.Context, dockerClient *client.Client, directory string, command []string, logFile *os.File) error {
	pullResult, err := dockerClient.ImagePull(ctx, "janoszen/terraform", types.ImagePullOptions{})
	if err != nil {
		log.Printf("failed to pull Terraform container image (%v)", err)
		_, err = io.Copy(logFile, pullResult)
		return err
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
		log.Printf("failed to create Terraform container (%v)", err)
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
			log.Printf("failed to remove container %s (%v)", terraformContainer.ID, err)
		}
	}()
	err = dockerClient.ContainerStart(ctx, terraformContainer.ID, types.ContainerStartOptions{})
	if err != nil {
		log.Printf("failed to start Terraform container (%v)", err)
		return err
	}
	containerOutput, err := dockerClient.ContainerLogs(ctx, terraformContainer.ID, types.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
	})
	if err != nil {
		log.Printf("failed to stream logs from Terraform container (%v)", err)
		return err
	}
	defer func() {
		_ = containerOutput.Close()
	}()
	logBuffer := bytes.NewBuffer(nil)
	_, err = stdcopy.StdCopy(logBuffer, logBuffer, containerOutput)
	if err != nil {
		log.Printf("failed to stream logs from Terraform container (%v)", err)
		return err
	}

	logs := logBuffer.Bytes()
	_, err = logFile.Write(logs)
	if err != nil {
		log.Printf("Failed to copy Docker buffer to log file (%v)", err)
		return err
	}

	inspect, err := dockerClient.ContainerInspect(ctx, terraformContainer.ID)
	if err != nil {
		log.Printf("failed to inspect Terraform container (%v)", err)
		return err
	}

	if inspect.State.ExitCode != 0 {
		log.Printf("terraform %s failed:\n---\n%s\n---\n", command[0], logs)
		return fmt.Errorf("terraform %s failed:\n---\n%s\n---\n", command[0], logBuffer.String())
	}

	return nil
}
