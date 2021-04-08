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

package validate

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/docker/docker/daemon/logger/jsonfilelog/jsonlog"
	internalapi "github.com/kubernetes-sigs/cri-tools/pkg/cri"
	"github.com/kubernetes-sigs/cri-tools/pkg/framework"
	runtimeapi "k8s.io/cri-api/pkg/apis/runtime/v1"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

// streamType is the type of the stream.
type streamType string

const (
	defaultStopContainerTimeout int64      = 60
	defaultExecSyncTimeout      int64      = 30
	defaultLog                  string     = "hello World"
	stdoutType                  streamType = "stdout"
	stderrType                  streamType = "stderr"
)

// logMessage is the internal log type.
type logMessage struct {
	timestamp time.Time
	stream    streamType
	log       string
}

var _ = framework.KubeDescribe("Container", func() {
	f := framework.NewDefaultCRIFramework()

	var rc internalapi.RuntimeService
	var ic internalapi.ImageManagerService

	BeforeEach(func() {
		rc = f.CRIClient.CRIRuntimeClient
		ic = f.CRIClient.CRIImageClient
	})

	Context("runtime should support basic operations on container", func() {
		var podID string
		var podConfig *runtimeapi.PodSandboxConfig

		BeforeEach(func() {
			podID, podConfig = framework.CreatePodSandboxForContainer(rc)
		})

		AfterEach(func() {
			By("stop PodSandbox")
			rc.StopPodSandbox(podID)
			By("delete PodSandbox")
			rc.RemovePodSandbox(podID)
		})

		It("runtime should support creating container [Conformance]", func() {
			By("test create a default container")
			containerID := framework.CreateDefaultContainer(rc, ic, podID, podConfig, "container-for-create-")

			By("test list container")
			containers := listContainerForID(rc, containerID)
			Expect(containerFound(containers, containerID)).To(BeTrue(), "Container should be created")
		})

		It("runtime should support starting container [Conformance]", func() {
			By("create container")
			containerID := framework.CreateDefaultContainer(rc, ic, podID, podConfig, "container-for-start-test-")

			By("test start container")
			testStartContainer(rc, containerID)
		})

		It("runtime should support stopping container [Conformance]", func() {
			By("create container")
			containerID := framework.CreateDefaultContainer(rc, ic, podID, podConfig, "container-for-stop-test-")

			By("start container")
			startContainer(rc, containerID)

			By("test stop container")
			testStopContainer(rc, containerID)
		})

		It("runtime should support removing created container [Conformance]", func() {
			By("create container")
			containerID := framework.CreatePauseContainer(rc, ic, podID, podConfig, "container-for-remove-created-test-")

			By("test remove container")
			removeContainer(rc, containerID)
			containers := listContainerForID(rc, containerID)
			Expect(containerFound(containers, containerID)).To(BeFalse(), "Container should be removed")
		})

		It("runtime should support removing running container [Conformance]", func() {
			By("create container")
			containerID := framework.CreatePauseContainer(rc, ic, podID, podConfig, "container-for-remove-running-test-")

			By("start container")
			startContainer(rc, containerID)

			By("test remove container")
			removeContainer(rc, containerID)
			containers := listContainerForID(rc, containerID)
			Expect(containerFound(containers, containerID)).To(BeFalse(), "Container should be removed")
		})

		It("runtime should support removing stopped container [Conformance]", func() {
			By("create container")
			containerID := framework.CreatePauseContainer(rc, ic, podID, podConfig, "container-for-remove-stopped-test-")

			By("start container")
			startContainer(rc, containerID)

			By("test stop container")
			testStopContainer(rc, containerID)

			By("test remove container")
			removeContainer(rc, containerID)
			containers := listContainerForID(rc, containerID)
			Expect(containerFound(containers, containerID)).To(BeFalse(), "Container should be removed")
		})

		It("runtime should support execSync [Conformance]", func() {
			By("create container")
			containerID := framework.CreateDefaultContainer(rc, ic, podID, podConfig, "container-for-execSync-test-")

			By("start container")
			startContainer(rc, containerID)

			By("test execSync")
			verifyExecSyncOutput(rc, containerID, echoHelloCmd, echoHelloOutput)
		})

		It("runtime should support execSync with timeout [Conformance]", func() {
			By("create container")
			containerID := framework.CreateDefaultContainer(rc, ic, podID, podConfig, "container-for-execSync-timeout-test-")

			By("start container")
			startContainer(rc, containerID)

			By("test execSync with timeout")
			_, _, err := rc.ExecSync(containerID, sleepCmd, time.Second)
			Expect(err).Should(HaveOccurred(), "execSync should timeout")

			By("timeout exec process should be gone")
			stdout, stderr, err := rc.ExecSync(containerID, checkSleepCmd,
				time.Duration(defaultExecSyncTimeout)*time.Second)
			framework.ExpectNoError(err)
			Expect(stderr).To(BeEmpty())
			Expect(strings.TrimSpace(string(stdout))).To(BeEmpty())
		})

		It("runtime should support listing container stats [Conformance]", func() {
			By("create container")
			containerID := framework.CreateDefaultContainer(rc, ic, podID, podConfig, "container-for-stats-")

			By("start container")
			startContainer(rc, containerID)

			By("test container stats")
			stats := listContainerStatsForID(rc, containerID)
			Expect(stats.Attributes.Id).To(Equal(containerID))
			Expect(stats.Attributes.Metadata.Name).To(ContainSubstring("container-for-stats-"))
			Expect(stats.Cpu.Timestamp).NotTo(BeZero())
			Expect(stats.Memory.Timestamp).NotTo(BeZero())
		})
	})

	Context("runtime should support adding volume and device", func() {
		var podID string
		var podConfig *runtimeapi.PodSandboxConfig

		BeforeEach(func() {
			podID, podConfig = framework.CreatePodSandboxForContainer(rc)
		})

		AfterEach(func() {
			By("stop PodSandbox")
			rc.StopPodSandbox(podID)
			By("delete PodSandbox")
			rc.RemovePodSandbox(podID)
		})

		It("runtime should support starting container with volume [Conformance]", func() {
			By("create host path and flag file")
			hostPath, _ := createHostPath(podID)

			defer os.RemoveAll(hostPath) // clean up the TempDir

			By("create container with volume")
			containerID := createVolumeContainer(rc, ic, "container-with-volume-test-", podID, podConfig, hostPath)

			By("test start container with volume")
			testStartContainer(rc, containerID)

			By("check whether 'hostPath' contains file or dir in container")
			output := execSyncContainer(rc, containerID, checkPathCmd(hostPath))
			Expect(len(output)).NotTo(BeZero(), "len(output) should not be zero.")
		})

		It("runtime should support starting container with volume when host path is a symlink [Conformance]", func() {
			By("create host path and flag file")
			hostPath, _ := createHostPath(podID)
			defer os.RemoveAll(hostPath) // clean up the TempDir

			By("create symlink")
			symlinkPath := createSymlink(hostPath)
			defer os.RemoveAll(symlinkPath) // clean up the symlink

			By("create volume container with symlink host path")
			containerID := createVolumeContainer(rc, ic, "container-with-symlink-host-path-test-", podID, podConfig, symlinkPath)

			By("test start volume container with symlink host path")
			testStartContainer(rc, containerID)

			By("check whether 'symlink' contains file or dir in container")
			output := execSyncContainer(rc, containerID, checkPathCmd(symlinkPath))
			Expect(len(output)).NotTo(BeZero(), "len(output) should not be zero.")
		})

		// TODO(random-liu): Decide whether to add host path not exist test when https://github.com/kubernetes/kubernetes/pull/61460
		// is finalized.
	})

	Context("runtime should support log", func() {
		var podID, hostPath string
		var podConfig *runtimeapi.PodSandboxConfig

		BeforeEach(func() {
			podID, podConfig, hostPath = createPodSandboxWithLogDirectory(rc)
		})

		AfterEach(func() {
			By("stop PodSandbox")
			rc.StopPodSandbox(podID)
			By("delete PodSandbox")
			rc.RemovePodSandbox(podID)
			By("clean up the TempDir")
			os.RemoveAll(hostPath)
		})

		It("runtime should support starting container with log [Conformance]", func() {
			By("create container with log")
			logPath, containerID := createLogContainer(rc, ic, "container-with-log-test-", podID, podConfig)

			By("start container with log")
			startContainer(rc, containerID)
			// wait container exited and check the status.
			Eventually(func() runtimeapi.ContainerState {
				return getContainerStatus(rc, containerID).State
			}, time.Minute, time.Second*4).Should(Equal(runtimeapi.ContainerState_CONTAINER_EXITED))

			By("check the log context")
			expectedLogMessage := defaultLog + "\n"
			verifyLogContents(podConfig, logPath, expectedLogMessage, stdoutType)
		})

		It("runtime should support reopening container log [Conformance]", func() {
			By("create container with log")
			logPath, containerID := createKeepLoggingContainer(rc, ic, "container-reopen-log-test-", podID, podConfig)

			By("start container with log")
			startContainer(rc, containerID)

			Eventually(func() []logMessage {
				return parseLogLine(podConfig, logPath)
			}, time.Minute, time.Second).ShouldNot(BeEmpty(), "container log should be generated")

			By("rename container log")
			newLogPath := logPath + ".new"
			Expect(os.Rename(filepath.Join(podConfig.LogDirectory, logPath),
				filepath.Join(podConfig.LogDirectory, newLogPath))).To(Succeed())

			By("reopen container log")
			Expect(rc.ReopenContainerLog(containerID)).To(Succeed())

			Expect(pathExists(filepath.Join(podConfig.LogDirectory, logPath))).To(
				BeTrue(), "new container log file should be created")
			Eventually(func() []logMessage {
				return parseLogLine(podConfig, logPath)
			}, time.Minute, time.Second).ShouldNot(BeEmpty(), "new container log should be generated")
			oldLength := len(parseLogLine(podConfig, newLogPath))
			Consistently(func() int {
				return len(parseLogLine(podConfig, newLogPath))
			}, 5*time.Second, time.Second).Should(Equal(oldLength), "old container log should not change")
		})
	})
})

// containerFound returns whether containers is found.
func containerFound(containers []*runtimeapi.Container, containerID string) bool {
	for _, container := range containers {
		if container.Id == containerID {
			return true
		}
	}
	return false
}

// getContainerStatus gets ContainerState for containerID and fails if it gets error.
func getContainerStatus(c internalapi.RuntimeService, containerID string) *runtimeapi.ContainerStatus {
	By("Get container status for containerID: " + containerID)
	status, err := c.ContainerStatus(containerID)
	framework.ExpectNoError(err, "failed to get container %q status: %v", containerID, err)
	return status
}

// createShellContainer creates a container to run /bin/sh.
func createShellContainer(rc internalapi.RuntimeService, ic internalapi.ImageManagerService, podID string, podConfig *runtimeapi.PodSandboxConfig, prefix string) string {
	containerName := prefix + framework.NewUUID()
	containerConfig := &runtimeapi.ContainerConfig{
		Metadata:  framework.BuildContainerMetadata(containerName, framework.DefaultAttempt),
		Image:     &runtimeapi.ImageSpec{Image: framework.DefaultContainerImage},
		Command:   shellCmd,
		Linux:     &runtimeapi.LinuxContainerConfig{},
		Stdin:     true,
		StdinOnce: true,
		Tty:       false,
	}

	return framework.CreateContainer(rc, ic, containerConfig, podID, podConfig)
}

// startContainer start the container for containerID.
func startContainer(c internalapi.RuntimeService, containerID string) {
	By("Start container for containerID: " + containerID)
	err := c.StartContainer(containerID)
	framework.ExpectNoError(err, "failed to start container: %v", err)
	framework.Logf("Started container %q\n", containerID)
}

// testStartContainer starts the container for containerID and make sure it's running.
func testStartContainer(rc internalapi.RuntimeService, containerID string) {
	startContainer(rc, containerID)
	Eventually(func() runtimeapi.ContainerState {
		return getContainerStatus(rc, containerID).State
	}, time.Minute, time.Second*4).Should(Equal(runtimeapi.ContainerState_CONTAINER_RUNNING))
}

// stopContainer stops the container for containerID.
func stopContainer(c internalapi.RuntimeService, containerID string, timeout int64) {
	By("Stop container for containerID: " + containerID)
	stopped := make(chan bool, 1)

	go func() {
		defer GinkgoRecover()
		err := c.StopContainer(containerID, timeout)
		framework.ExpectNoError(err, "failed to stop container: %v", err)
		stopped <- true
	}()

	select {
	case <-time.After(time.Duration(timeout) * time.Second):
		framework.Failf("stop container %q timeout.\n", containerID)
	case <-stopped:
		framework.Logf("Stopped container %q\n", containerID)
	}
}

// testStopContainer stops the container for containerID and make sure it's exited.
func testStopContainer(c internalapi.RuntimeService, containerID string) {
	stopContainer(c, containerID, defaultStopContainerTimeout)
	Eventually(func() runtimeapi.ContainerState {
		return getContainerStatus(c, containerID).State
	}, time.Minute, time.Second*4).Should(Equal(runtimeapi.ContainerState_CONTAINER_EXITED))
}

// removeContainer removes the container for containerID.
func removeContainer(c internalapi.RuntimeService, containerID string) {
	By("Remove container for containerID: " + containerID)
	err := c.RemoveContainer(containerID)
	framework.ExpectNoError(err, "failed to remove container: %v", err)
	framework.Logf("Removed container %q\n", containerID)
}

// listContainerForID lists container for containerID.
func listContainerForID(c internalapi.RuntimeService, containerID string) []*runtimeapi.Container {
	By("List containers for containerID: " + containerID)
	filter := &runtimeapi.ContainerFilter{
		Id: containerID,
	}
	containers, err := c.ListContainers(filter)
	framework.ExpectNoError(err, "failed to list containers %q status: %v", containerID, err)
	return containers
}

// execSyncContainer test execSync for containerID and make sure the response is right.
func execSyncContainer(c internalapi.RuntimeService, containerID string, command []string) string {
	By("execSync for containerID: " + containerID)
	stdout, stderr, err := c.ExecSync(containerID, command, time.Duration(defaultExecSyncTimeout)*time.Second)
	framework.ExpectNoError(err, "failed to execSync in container %q", containerID)
	Expect(stderr).To(BeNil(), "The stderr should be nil.")
	framework.Logf("Execsync succeed")

	return string(stdout)
}

// execSyncContainer test execSync for containerID and make sure the response is right.
func verifyExecSyncOutput(c internalapi.RuntimeService, containerID string, command []string, expectedLogMessage string) {
	By("verify execSync output")
	stdout := execSyncContainer(c, containerID, command)
	Expect(stdout).To(Equal(expectedLogMessage), "The stdout output of execSync should be %s", expectedLogMessage)
	framework.Logf("verify Execsync output succeed")
}

// createHostPath creates the hostPath and flagFile for volume.
func createHostPath(podID string) (string, string) {
	hostPath, err := ioutil.TempDir("", "test"+podID)
	framework.ExpectNoError(err, "failed to create TempDir %q: %v", hostPath, err)

	flagFile := "testVolume.file"
	_, err = os.Create(filepath.Join(hostPath, flagFile))
	framework.ExpectNoError(err, "failed to create volume file %q: %v", flagFile, err)

	return hostPath, flagFile
}

// createSymlink creates a symlink of path.
func createSymlink(path string) string {
	symlinkPath := path + "-symlink"
	framework.ExpectNoError(os.Symlink(path, symlinkPath), "failed to create symlink %q", symlinkPath)
	return symlinkPath
}

// createVolumeContainer creates a container with volume and the prefix of containerName and fails if it gets error.
func createVolumeContainer(rc internalapi.RuntimeService, ic internalapi.ImageManagerService, prefix string, podID string, podConfig *runtimeapi.PodSandboxConfig, hostPath string) string {
	By("create a container with volume and name")
	containerName := prefix + framework.NewUUID()
	containerConfig := &runtimeapi.ContainerConfig{
		Metadata: framework.BuildContainerMetadata(containerName, framework.DefaultAttempt),
		Image:    &runtimeapi.ImageSpec{Image: framework.DefaultContainerImage},
		Command:  pauseCmd,
		// mount host path to the same directory in container, and will check if hostPath isn't empty
		Mounts: []*runtimeapi.Mount{
			{
				HostPath:       hostPath,
				ContainerPath:  hostPath,
				SelinuxRelabel: true,
			},
		},
	}

	return framework.CreateContainer(rc, ic, containerConfig, podID, podConfig)
}

// createLogContainer creates a container with log and the prefix of containerName.
func createLogContainer(rc internalapi.RuntimeService, ic internalapi.ImageManagerService, prefix string, podID string, podConfig *runtimeapi.PodSandboxConfig) (string, string) {
	By("create a container with log and name")
	containerName := prefix + framework.NewUUID()
	path := fmt.Sprintf("%s.log", containerName)
	containerConfig := &runtimeapi.ContainerConfig{
		Metadata: framework.BuildContainerMetadata(containerName, framework.DefaultAttempt),
		Image:    &runtimeapi.ImageSpec{Image: framework.DefaultContainerImage},
		Command:  logDefaultCmd,
		LogPath:  path,
	}
	return containerConfig.LogPath, framework.CreateContainer(rc, ic, containerConfig, podID, podConfig)
}

// createKeepLoggingContainer creates a container keeps logging defaultLog to output.
func createKeepLoggingContainer(rc internalapi.RuntimeService, ic internalapi.ImageManagerService, prefix string, podID string, podConfig *runtimeapi.PodSandboxConfig) (string, string) {
	By("create a container with log and name")
	containerName := prefix + framework.NewUUID()
	path := fmt.Sprintf("%s.log", containerName)
	containerConfig := &runtimeapi.ContainerConfig{
		Metadata: framework.BuildContainerMetadata(containerName, framework.DefaultAttempt),
		Image:    &runtimeapi.ImageSpec{Image: framework.DefaultContainerImage},
		Command:  loopLogDefaultCmd,
		LogPath:  path,
	}
	return containerConfig.LogPath, framework.CreateContainer(rc, ic, containerConfig, podID, podConfig)
}

// pathExists check whether 'path' does exist or not
func pathExists(path string) bool {
	_, err := os.Stat(path)
	if err == nil {
		return true
	}
	if os.IsNotExist(err) {
		return false
	}
	framework.ExpectNoError(err, "failed to check whether %q Exists: %v", path, err)
	return false
}

// parseDockerJSONLog parses logs in Docker JSON log format.
// Docker JSON log format example:
//   {"log":"content 1","stream":"stdout","time":"2016-10-20T18:39:20.57606443Z"}
//   {"log":"content 2","stream":"stderr","time":"2016-10-20T18:39:20.57606444Z"}
func parseDockerJSONLog(log []byte, msg *logMessage) {
	var l jsonlog.JSONLog

	err := json.Unmarshal(log, &l)
	framework.ExpectNoError(err, "failed with %v to unmarshal log %q", err, l)

	msg.timestamp = l.Created
	msg.stream = streamType(l.Stream)
	msg.log = l.Log
}

// parseCRILog parses logs in CRI log format.
// CRI log format example :
//   2016-10-06T00:17:09.669794202Z stdout P The content of the log entry 1
//   2016-10-06T00:17:10.113242941Z stderr F The content of the log entry 2
func parseCRILog(log string, msg *logMessage) {
	logMessage := strings.SplitN(log, " ", 4)
	if len(log) < 4 {
		err := errors.New("invalid CRI log")
		framework.ExpectNoError(err, "failed to parse CRI log: %v", err)
	}
	timeStamp, err := time.Parse(time.RFC3339Nano, logMessage[0])
	framework.ExpectNoError(err, "failed to parse timeStamp: %v", err)
	stream := logMessage[1]

	msg.timestamp = timeStamp
	msg.stream = streamType(stream)
	// Skip the tag field.
	msg.log = logMessage[3] + "\n"
}

// parseLogLine parses log by row.
func parseLogLine(podConfig *runtimeapi.PodSandboxConfig, logPath string) []logMessage {
	path := filepath.Join(podConfig.LogDirectory, logPath)
	f, err := os.Open(path)
	framework.ExpectNoError(err, "failed to open log file: %v", err)
	framework.Logf("Open log file %s", path)
	defer f.Close()

	var msg logMessage
	var msgLog []logMessage

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()

		// to determine whether the log is Docker format or CRI format.
		if strings.HasPrefix(line, "{") {
			parseDockerJSONLog([]byte(line), &msg)
		} else {
			parseCRILog(line, &msg)
		}

		msgLog = append(msgLog, msg)
	}

	if err := scanner.Err(); err != nil {
		framework.ExpectNoError(err, "failed to read log by row: %v", err)
	}
	framework.Logf("Parse container log succeed")

	return msgLog
}

// verifyLogContents verifies the contents of container log.
func verifyLogContents(podConfig *runtimeapi.PodSandboxConfig, logPath string, log string, stream streamType) {
	By("verify log contents")
	msgs := parseLogLine(podConfig, logPath)

	found := false
	for _, msg := range msgs {
		if msg.log == log && msg.stream == stream {
			found = true
			break
		}
	}
	Expect(found).To(BeTrue(), "expected log %q (stream=%q) not found in logs %+v", log, stream, msgs)
}

// listContainerStatsForID lists container for containerID.
func listContainerStatsForID(c internalapi.RuntimeService, containerID string) *runtimeapi.ContainerStats {
	By("List container stats for containerID: " + containerID)
	stats, err := c.ContainerStats(containerID)
	framework.ExpectNoError(err, "failed to list container stats for %q status: %v", containerID, err)
	return stats
}
