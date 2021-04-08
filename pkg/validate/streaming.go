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
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	internalapi "github.com/kubernetes-sigs/cri-tools/pkg/cri"
	"github.com/kubernetes-sigs/cri-tools/pkg/framework"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	remoteclient "k8s.io/client-go/tools/remotecommand"
	"k8s.io/client-go/transport/spdy"
	runtimeapi "k8s.io/cri-api/pkg/apis/runtime/v1"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

const defaultExecStdinCloseTimeout = 20 * time.Second

var _ = framework.KubeDescribe("Streaming", func() {
	f := framework.NewDefaultCRIFramework()

	var rc internalapi.RuntimeService
	var ic internalapi.ImageManagerService

	BeforeEach(func() {
		rc = f.CRIClient.CRIRuntimeClient
		ic = f.CRIClient.CRIImageClient
	})

	Context("runtime should support streaming interfaces", func() {
		var podID string
		var podConfig *runtimeapi.PodSandboxConfig

		AfterEach(func() {
			By("stop PodSandbox")
			rc.StopPodSandbox(podID)
			By("delete PodSandbox")
			rc.RemovePodSandbox(podID)
		})

		It("runtime should support exec with tty=false and stdin=false [Conformance]", func() {
			podID, podConfig = framework.CreatePodSandboxForContainer(rc)

			By("create a default container")
			containerID := framework.CreateDefaultContainer(rc, ic, podID, podConfig, "container-for-exec-test")

			By("start container")
			startContainer(rc, containerID)

			execReq := &runtimeapi.ExecRequest{
				ContainerId: containerID,
				Cmd:         echoHelloCmd,
				Stdout:      true,
				Stderr:      true,
			}
			req := createExec(rc, execReq)

			By("check the output of exec")
			checkExec(rc, req, echoHelloOutput, true, false)
		})

		It("runtime should support exec with tty=true and stdin=true [Conformance]", func() {
			podID, podConfig = framework.CreatePodSandboxForContainer(rc)

			By("create a default container")
			containerID := framework.CreateDefaultContainer(rc, ic, podID, podConfig, "container-for-exec-test")

			By("start container")
			startContainer(rc, containerID)

			execReq := &runtimeapi.ExecRequest{
				ContainerId: containerID,
				Cmd:         echoHelloCmd,
				Stdout:      true,
				Tty:         true,
				Stdin:       true,
			}
			req := createExec(rc, execReq)

			By("check the output of exec")
			checkExec(rc, req, "hello", false, true)
		})

		It("runtime should support attach [Conformance]", func() {
			podID, podConfig = framework.CreatePodSandboxForContainer(rc)

			By("create a default container")
			containerID := createShellContainer(rc, ic, podID, podConfig, "container-for-attach-test")

			By("start container")
			startContainer(rc, containerID)

			req := createDefaultAttach(rc, containerID)

			By("check the output of attach")
			checkAttach(rc, req)
		})

		It("runtime should support portforward [Conformance]", func() {
			By("create a PodSandbox with container port port mapping")
			var podConfig *runtimeapi.PodSandboxConfig
			portMappings := []*runtimeapi.PortMapping{
				{
					ContainerPort: webServerContainerPort,
				},
			}
			podID, podConfig = createPodSandboxWithPortMapping(rc, portMappings, false)

			By("create a web server container")
			containerID := createWebServerContainer(rc, ic, podID, podConfig, "container-for-portforward-test")

			By("start the web server container")
			startContainer(rc, containerID)

			req := createDefaultPortForward(rc, podID)

			By("check the output of portforward")
			checkPortForward(rc, req, webServerHostPortForPortForward, webServerContainerPort)
		})

	})
})

func createExec(c internalapi.RuntimeService, execReq *runtimeapi.ExecRequest) string {
	By("exec given command in container: " + execReq.ContainerId)
	resp, err := c.Exec(execReq)
	framework.ExpectNoError(err, "failed to exec in container %q", execReq.ContainerId)
	framework.Logf("Get exec url: " + resp.Url)
	return resp.Url
}

func checkExec(c internalapi.RuntimeService, execServerURL, stdout string, stdoutExactMatch bool, isTty bool) {
	var (
		localOut                  = &safeBuffer{buffer: bytes.Buffer{}}
		localErr                  = &safeBuffer{buffer: bytes.Buffer{}}
		localInRead, localInWrite = io.Pipe()
		testDone                  = make(chan struct{})
		wg                        sync.WaitGroup
	)

	wg.Add(1)
	// Wait until output read and then shutdown localIn pipe.
	go func() {
		defer wg.Done()
		defer localInWrite.Close()
		ticker := time.NewTicker(defaultExecStdinCloseTimeout)
		select {
		case <-testDone:
		case <-ticker.C:
		}
	}()
	defer func() {
		close(testDone)
		wg.Wait()
	}()

	// Only http is supported now.
	// TODO: support streaming APIs via tls.
	url := parseURL(c, execServerURL)
	e, err := remoteclient.NewSPDYExecutor(&rest.Config{TLSClientConfig: rest.TLSClientConfig{Insecure: true}}, "POST", url)
	framework.ExpectNoError(err, "failed to create executor for %q", execServerURL)

	streamOptions := remoteclient.StreamOptions{
		Stdout: localOut,
		Stderr: localErr,
		Tty:    false,
	}
	if isTty {
		streamOptions.Stdin = localInRead
		streamOptions.Tty = true
	}
	err = e.Stream(streamOptions)
	framework.ExpectNoError(err, "failed to open streamer for %q", execServerURL)

	if stdoutExactMatch {
		Expect(localOut.String()).To(Equal(stdout), "The stdout of exec should be "+stdout)
	} else {
		Expect(localOut.String()).To(ContainSubstring(stdout), "The stdout of exec should contain "+stdout)
	}
	Expect(localErr.String()).To(BeEmpty(), "The stderr of exec should be empty")
	framework.Logf("Check exec url %q succeed", execServerURL)
}

func parseURL(c internalapi.RuntimeService, serverURL string) *url.URL {
	url, err := url.Parse(serverURL)
	framework.ExpectNoError(err, "failed to parse url:  %q", serverURL)

	version := getVersion(c)
	if version.RuntimeName == "docker" {
		if url.Host == "" {
			url.Host = defaultStreamServerAddress
		}
		if url.Scheme == "" {
			url.Scheme = defaultStreamServerScheme
		}
	}

	Expect(url.Host).NotTo(BeEmpty(), "The host of url should not be empty")
	framework.Logf("Parse url %q succeed", serverURL)
	return url
}

func createDefaultAttach(c internalapi.RuntimeService, containerID string) string {
	By("attach container: " + containerID)
	req := &runtimeapi.AttachRequest{
		ContainerId: containerID,
		Stdin:       true,
		Stdout:      true,
		Stderr:      true,
		Tty:         false,
	}

	resp, err := c.Attach(req)
	framework.ExpectNoError(err, "failed to attach in container %q", containerID)
	framework.Logf("Get attach url: " + resp.Url)
	return resp.Url
}

// safeBuffer is a goroutine safe bytes.Buffer
type safeBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

// Write appends the contents of p to the buffer, growing the buffer as needed. It returns
// the number of bytes written.
func (s *safeBuffer) Write(p []byte) (n int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buffer.Write(p)
}

// String returns the contents of the unread portion of the buffer
// as a string.  If the Buffer is a nil pointer, it returns "<nil>".
func (s *safeBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buffer.String()
}

func checkAttach(c internalapi.RuntimeService, attachServerURL string) {
	localOut := &safeBuffer{buffer: bytes.Buffer{}}
	localErr := &safeBuffer{buffer: bytes.Buffer{}}
	reader, writer := io.Pipe()
	go func() {
		defer GinkgoRecover()
		defer writer.Close()
		header := localOut.String()
		time.Sleep(1 * time.Second)
		Eventually(func() bool {
			oldHeader := header
			header = localOut.String()
			return len(header) == len(oldHeader)
		}, 10*time.Second, time.Second).Should(BeTrue(), "The container should stop output when there is no input")
		writer.Write([]byte(strings.Join(echoHelloCmd, " ") + "\n"))
		Eventually(func() string {
			return strings.TrimPrefix(localOut.String(), header)
		}, time.Minute, time.Second).Should(Equal(attachEchoHelloOutput), "The stdout of attach should be hello")
		Consistently(func() string {
			return strings.TrimPrefix(localOut.String(), header)
		}, 3*time.Second, time.Second).Should(Equal(attachEchoHelloOutput), "The stdout of attach should not contain other things")
	}()

	// Only http is supported now.
	// TODO: support streaming APIs via tls.
	url := parseURL(c, attachServerURL)
	e, err := remoteclient.NewSPDYExecutor(&rest.Config{TLSClientConfig: rest.TLSClientConfig{Insecure: true}}, "POST", url)
	framework.ExpectNoError(err, "failed to create executor for %q", attachServerURL)

	err = e.Stream(remoteclient.StreamOptions{
		Stdin:  reader,
		Stdout: localOut,
		Stderr: localErr,
		Tty:    false,
	})
	framework.ExpectNoError(err, "failed to open streamer for %q", attachServerURL)

	Expect(localErr.String()).To(BeEmpty(), "The stderr of attach should be empty")
	framework.Logf("Check attach url %q succeed", attachServerURL)
}

func createDefaultPortForward(c internalapi.RuntimeService, podID string) string {
	By("port forward PodSandbox: " + podID)
	req := &runtimeapi.PortForwardRequest{
		PodSandboxId: podID,
	}

	resp, err := c.PortForward(req)
	framework.ExpectNoError(err, "failed to port forward PodSandbox %q", podID)
	framework.Logf("Get port forward url: " + resp.Url)
	return resp.Url
}

func checkPortForward(c internalapi.RuntimeService, portForwardSeverURL string, hostPort, containerPort int32) {
	stopChan := make(chan struct{}, 1)
	readyChan := make(chan struct{})
	defer close(stopChan)

	transport, upgrader, err := spdy.RoundTripperFor(&rest.Config{TLSClientConfig: rest.TLSClientConfig{Insecure: true}})
	framework.ExpectNoError(err, "failed to create spdy round tripper")
	url := parseURL(c, portForwardSeverURL)
	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, "POST", url)
	pf, err := portforward.New(dialer, []string{fmt.Sprintf("%d:%d", hostPort, containerPort)}, stopChan, readyChan, os.Stdout, os.Stderr)
	framework.ExpectNoError(err, "failed to create port forward for %q", portForwardSeverURL)

	go func() {
		defer GinkgoRecover()
		By("start port forward")
		err = pf.ForwardPorts()
		framework.ExpectNoError(err, "failed to start port forward for %q", portForwardSeverURL)
	}()

	By(fmt.Sprintf("check if we can get nginx main page via localhost:%d", hostPort))
	checkMainPage(c, "", hostPort)
	framework.Logf("Check port forward url %q succeed", portForwardSeverURL)
}
