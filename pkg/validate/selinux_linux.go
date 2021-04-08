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
	"fmt"
	"strings"
	"time"

	internalapi "github.com/kubernetes-sigs/cri-tools/pkg/cri"
	"github.com/kubernetes-sigs/cri-tools/pkg/framework"
	selinux "github.com/opencontainers/selinux/go-selinux"
	runtimeapi "k8s.io/cri-api/pkg/apis/runtime/v1"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = framework.KubeDescribe("SELinux", func() {
	f := framework.NewDefaultCRIFramework()

	var rc internalapi.RuntimeService
	var ic internalapi.ImageManagerService

	if selinux.GetEnabled() {
		BeforeEach(func() {
			rc = f.CRIClient.CRIRuntimeClient
			ic = f.CRIClient.CRIImageClient
		})

		Context("runtime should support selinux", func() {
			var sandboxID string
			var sandboxConfig *runtimeapi.PodSandboxConfig

			var sandboxTests = func(privileged bool) {
				It("should work with just selinux level set", func() {
					options := &runtimeapi.SELinuxOption{
						Level: "s0",
					}
					containerID := createContainerWithSelinux(rc, ic, sandboxID, sandboxConfig, options, privileged, true, true)
					checkContainerSelinux(rc, containerID, true)
				})

				It("should work with selinux set", func() {
					options := &runtimeapi.SELinuxOption{
						User:  "system_u",
						Role:  "system_r",
						Type:  "svirt_lxc_net_t",
						Level: "s0:c4,c5",
					}
					containerID := createContainerWithSelinux(rc, ic, sandboxID, sandboxConfig, options, privileged, true, true)
					checkContainerSelinux(rc, containerID, true)
				})

				It("should error on create with wrong options", func() {
					options := &runtimeapi.SELinuxOption{
						User: "system_u",
						Role: "system_r",
						Type: "svirt_lxc_net_t",
						// s0,c4,c5 is wrong, should have been s0:c4,c5
						Level: "s0,c4,c5",
					}
					createContainerWithSelinux(rc, ic, sandboxID, sandboxConfig, options, privileged, false, false)
				})

				It("mount label should have correct role and type", func() {
					containerID := createContainerWithSelinux(rc, ic, sandboxID, sandboxConfig, nil, privileged, true, true)
					checkMountLabelRoleType(rc, containerID)
				})

				It("mount label should have category", func() {
					containerID := createContainerWithSelinux(rc, ic, sandboxID, sandboxConfig, nil, privileged, true, true)
					checkMountLabelMCS(rc, containerID)
				})

				It("process label should have correct role and type", func() {
					containerID := createContainerWithSelinux(rc, ic, sandboxID, sandboxConfig, nil, privileged, true, true)
					checkProcessLabelRoleType(rc, containerID, privileged)
				})

				It("process label should have category", func() {
					containerID := createContainerWithSelinux(rc, ic, sandboxID, sandboxConfig, nil, privileged, true, true)
					checkProcessLabelMCS(rc, containerID, privileged)
				})

				It("should create containers with the same process label", func() {
					containerID := createContainerWithSelinux(rc, ic, sandboxID, sandboxConfig, nil, privileged, true, true)
					containerID2 := createContainerWithSelinux(rc, ic, sandboxID, sandboxConfig, nil, privileged, true, true)
					label1 := checkProcessLabelMCS(rc, containerID, privileged)
					label2 := checkProcessLabelMCS(rc, containerID2, privileged)
					Expect(label1).To(Equal(label2))
				})
			}

			Context("when single pod sandbox is not privileged", func() {
				BeforeEach(func() {
					sandboxID, sandboxConfig = framework.CreatePodSandboxForContainer(rc)
				})

				AfterEach(func() {
					cleanupSandbox(rc, sandboxID)
				})

				sandboxTests(false)
			})

			Context("when single pod sandbox is privileged", func() {
				BeforeEach(func() {
					sandboxID, sandboxConfig = createPrivilegedPodSandbox(rc, true)
				})

				AfterEach(func() {
					cleanupSandbox(rc, sandboxID)
				})

				sandboxTests(true)
			})

			Context("when multiple pod sandboxes are not privileged", func() {
				var sandboxID2 string
				var sandboxConfig2 *runtimeapi.PodSandboxConfig

				BeforeEach(func() {
					sandboxID, sandboxConfig = framework.CreatePodSandboxForContainer(rc)
					sandboxID2, sandboxConfig2 = framework.CreatePodSandboxForContainer(rc)
				})

				AfterEach(func() {
					cleanupSandbox(rc, sandboxID)
					cleanupSandbox(rc, sandboxID2)
				})

				It("should create containers with different process labels", func() {
					containerID := createContainerWithSelinux(rc, ic, sandboxID, sandboxConfig, nil, false, true, true)
					containerID2 := createContainerWithSelinux(rc, ic, sandboxID2, sandboxConfig2, nil, false, true, true)
					label1 := checkProcessLabelMCS(rc, containerID, false)
					label2 := checkProcessLabelMCS(rc, containerID2, false)
					Expect(label1).NotTo(Equal(label2))
				})
			})
		})
	}
})

func createContainerWithSelinux(rc internalapi.RuntimeService, ic internalapi.ImageManagerService, sandboxID string, sandboxConfig *runtimeapi.PodSandboxConfig, options *runtimeapi.SELinuxOption, privileged bool, shouldStart, shouldCreate bool) string {
	By("create a container with selinux")
	containerName := "selinux-test-" + framework.NewUUID()
	containerConfig := &runtimeapi.ContainerConfig{
		Metadata: framework.BuildContainerMetadata(containerName, framework.DefaultAttempt),
		Image:    &runtimeapi.ImageSpec{Image: framework.DefaultContainerImage},
		Command:  pauseCmd,
		Linux: &runtimeapi.LinuxContainerConfig{
			SecurityContext: &runtimeapi.LinuxContainerSecurityContext{
				SelinuxOptions: options,
				Privileged:     privileged,
			},
		},
	}
	containerID, err := framework.CreateContainerWithError(rc, ic, containerConfig, sandboxID, sandboxConfig)
	if !shouldCreate {
		Expect(err).To(HaveOccurred())
		return ""
	}

	Expect(err).NotTo(HaveOccurred())

	By("start container with selinux")
	err = rc.StartContainer(containerID)
	if shouldStart {
		Expect(err).NotTo(HaveOccurred())
	} else {
		Expect(err).To(HaveOccurred())
	}

	// wait container running
	Eventually(func() runtimeapi.ContainerState {
		return getContainerStatus(rc, containerID).State
	}, time.Minute, time.Second*4).Should(Equal(runtimeapi.ContainerState_CONTAINER_RUNNING))

	return containerID
}

func checkContainerSelinux(rc internalapi.RuntimeService, containerID string, shoudRun bool) {
	By("get container status")
	status, err := rc.ContainerStatus(containerID)
	Expect(err).NotTo(HaveOccurred())

	if shoudRun {
		Expect(status.GetExitCode()).To(Equal(int32(0)))
	} else {
		Expect(status.GetExitCode()).NotTo(Equal(int32(0)))
		return
	}

	cmd := []string{"touch", "foo"}
	stdout, stderr, err := rc.ExecSync(containerID, cmd, time.Duration(defaultExecSyncTimeout)*time.Second)
	msg := fmt.Sprintf("cmd %v, stdout %q, stderr %q", cmd, stdout, stderr)
	Expect(err).NotTo(HaveOccurred(), msg)
}

func cleanupSandbox(rc internalapi.RuntimeService, sandboxID string) {
	By("stop PodSandbox")
	rc.StopPodSandbox(sandboxID)
	By("delete PodSandbox")
	rc.RemovePodSandbox(sandboxID)
}

func checkMountLabelRoleType(rc internalapi.RuntimeService, containerID string) {
	// Check that the mount label policy is correct
	cmd := []string{"cat", "/proc/1/mountinfo"}
	stdout, stderr, err := rc.ExecSync(containerID, cmd, time.Duration(defaultExecSyncTimeout)*time.Second)
	msg := fmt.Sprintf("cmd %v, stdout %q, stderr %q", cmd, stdout, stderr)
	Expect(err).NotTo(HaveOccurred(), msg)
	Expect(string(stdout)).To(ContainSubstring(":object_r:container_file_t:"))
}

func checkProcessLabelRoleType(rc internalapi.RuntimeService, containerID string, privileged bool) {
	// Check that the process label policy is correct
	cmd := []string{"cat", "/proc/self/attr/current"}
	stdout, stderr, err := rc.ExecSync(containerID, cmd, time.Duration(defaultExecSyncTimeout)*time.Second)
	label := strings.Trim(string(stdout), "\x00")
	msg := fmt.Sprintf("cmd %v, stdout %q, stderr %q", cmd, stdout, stderr)
	Expect(err).NotTo(HaveOccurred(), msg)
	if privileged {
		Expect(label).To(ContainSubstring(":system_r:spc_t:"))
	} else {
		Expect(label).To(ContainSubstring(":system_r:container_t:"))
	}
}

func checkMountLabelMCS(rc internalapi.RuntimeService, containerID string) {
	// Check that the mount label MCS is correct
	cmd := []string{"cat", "/proc/1/mountinfo"}
	stdout, stderr, err := rc.ExecSync(containerID, cmd, time.Duration(defaultExecSyncTimeout)*time.Second)
	msg := fmt.Sprintf("cmd %v, stdout %q, stderr %q", cmd, stdout, stderr)
	Expect(err).NotTo(HaveOccurred(), msg)
	// check that a mount exists with MCS, where level is always s0 and there are two or more categories
	Expect(string(stdout)).To(MatchRegexp(`,context="[^"]*:s0(-s0)?:c[0-9]+(,c[0-9]+)+",`))
}

func checkProcessLabelMCS(rc internalapi.RuntimeService, containerID string, privileged bool) string {
	// Check that the process label MCS is correct
	cmd := []string{"cat", "/proc/self/attr/current"}
	stdout, stderr, err := rc.ExecSync(containerID, cmd, time.Duration(defaultExecSyncTimeout)*time.Second)
	label := strings.Trim(string(stdout), "\x00")
	msg := fmt.Sprintf("cmd %v, stdout %q, stderr %q", cmd, stdout, stderr)
	Expect(err).NotTo(HaveOccurred(), msg)
	if privileged {
		// check that a process label exists with optional MCS, where level is always s0 and we permit all categories
		Expect(label).To(MatchRegexp(`:s0(-s0)?(:c0\.c1023)?$`))
	} else {
		// check that a process label exists with MCS, where level is always s0 and there are two or more categories
		Expect(label).To(MatchRegexp(`:s0(-s0)?:c[0-9]+(,c[0-9]+)+$`))
	}
	return label
}
