/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/Sirupsen/logrus"
	"github.com/urfave/cli"
	"golang.org/x/net/context"
	remotecommandconsts "k8s.io/apimachinery/pkg/util/remotecommand"
	restclient "k8s.io/client-go/rest"
	remoteclient "k8s.io/client-go/tools/remotecommand"
	pb "k8s.io/kubernetes/pkg/kubelet/apis/cri/v1alpha1/runtime"
)

const (
	// TODO: make this configurable in kubelet.
	kubeletURLPrefix = "http://127.0.0.1:10250"
)

var runtimeExecCommand = cli.Command{
	Name:      "exec",
	Usage:     "exec a command in a container",
	ArgsUsage: "containerID COMMAND [ARG...]",
	Flags: []cli.Flag{
		cli.BoolFlag{
			Name:  "sync",
			Usage: "run a command in a container synchronously",
		},
		cli.Int64Flag{
			Name:  "timeout",
			Value: 0,
			Usage: "timeout for the command",
		},
		cli.BoolFlag{
			Name:  "tty, t",
			Usage: "exec a command in a tty",
		},
		cli.BoolFlag{
			Name:  "stdin, i",
			Usage: "stream stdin",
		},
	},
	Action: func(context *cli.Context) error {
		if len(context.Args()) < 2 {
			return cli.ShowSubcommandHelp(context)
		}

		if err := getRuntimeClient(context); err != nil {
			return err
		}

		var opts = execOptions{
			id:      context.Args().First(),
			timeout: context.Int64("timeout"),
			tty:     context.Bool("tty"),
			stdin:   context.Bool("stdin"),
			cmd:     context.Args()[1:],
		}
		if context.Bool("sync") {
			err := ExecSync(runtimeClient, opts)
			if err != nil {
				return fmt.Errorf("execing command in container synchronously failed: %v", err)
			}
			return nil
		}
		err := Exec(runtimeClient, opts)
		if err != nil {
			return fmt.Errorf("execing command in container failed: %v", err)
		}
		return nil
	},
	After: closeConnection,
}

// ExecSync sends an ExecSyncRequest to the server, and parses
// the returned ExecSyncResponse.
func ExecSync(client pb.RuntimeServiceClient, opts execOptions) error {
	request := &pb.ExecSyncRequest{
		ContainerId: opts.id,
		Cmd:         opts.cmd,
		Timeout:     opts.timeout,
	}
	logrus.Debugf("ExecSyncRequest: %v", request)
	r, err := client.ExecSync(context.Background(), request)
	logrus.Debugf("ExecSyncResponse: %v", r)
	if err != nil {
		return err
	}
	fmt.Println(string(r.Stdout))
	fmt.Println(string(r.Stderr))
	if r.ExitCode != 0 {
		fmt.Printf("Exit code: %v\n", r.ExitCode)
	}

	return nil
}

// Exec sends an ExecRequest to server, and parses the returned ExecResponse
func Exec(client pb.RuntimeServiceClient, opts execOptions) error {
	request := &pb.ExecRequest{
		ContainerId: opts.id,
		Cmd:         opts.cmd,
		Tty:         opts.tty,
		Stdin:       opts.stdin,
	}
	logrus.Debugf("ExecRequest: %v", request)
	r, err := client.Exec(context.Background(), request)
	logrus.Debugf("ExecResponse: %v", r)
	if err != nil {
		return err
	}
	execURL := r.Url
	if !strings.HasPrefix(execURL, "http") {
		execURL = kubeletURLPrefix + execURL

	}

	URL, err := url.Parse(execURL)
	if err != nil {
		return err
	}

	logrus.Debugf("Exec URL: %v", URL)
	exec, err := remoteclient.NewExecutor(&restclient.Config{}, "POST", URL)
	if err != nil {
		return err
	}

	streamOptions := remoteclient.StreamOptions{
		SupportedProtocols: remotecommandconsts.SupportedStreamingProtocols,
		Stdout:             os.Stdout,
		Stderr:             os.Stderr,
		Tty:                opts.tty,
	}
	if opts.stdin {
		streamOptions.Stdin = os.Stdin
	}
	logrus.Debugf("StreamOptions: %v", streamOptions)
	return exec.Stream(streamOptions)
}
