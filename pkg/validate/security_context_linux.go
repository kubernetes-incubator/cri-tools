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
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/kubernetes-sigs/cri-tools/pkg/framework"
	"github.com/pkg/errors"
	internalapi "k8s.io/cri-api/pkg/apis"
	runtimeapi "k8s.io/cri-api/pkg/apis/runtime/v1alpha2"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

const (
	nginxContainerImage string = "nginx:1.18"
	noNewPrivsImage     string = "gcr.io/kubernetes-e2e-test-images/nonewprivs:1.1"
)

var _ = framework.KubeDescribe("Security Context", func() {
	f := framework.NewDefaultCRIFramework()

	var rc internalapi.RuntimeService
	var ic internalapi.ImageManagerService
	var podID, podLogDir string
	var dirToCleanup []string
	var podConfig *runtimeapi.PodSandboxConfig

	BeforeEach(func() {
		rc = f.CRIClient.CRIRuntimeClient
		ic = f.CRIClient.CRIImageClient
	})

	AfterEach(func() {
		if podID != "" {
			By("stop PodSandbox")
			rc.StopPodSandbox(podID)
			By("delete PodSandbox")
			rc.RemovePodSandbox(podID)
		}
		if podLogDir != "" {
			os.RemoveAll(podLogDir)
		}
		for _, dir := range dirToCleanup {
			os.RemoveAll(dir)
		}
	})

	Context("NamespaceOption", func() {
		podSandboxName := "NamespaceOption-PodSandbox-" + framework.NewUUID()

		It("runtime should support HostPID", func() {
			By("create podSandbox for security context HostPID")
			namespaceOption := &runtimeapi.NamespaceOption{
				Pid:     runtimeapi.NamespaceMode_NODE,
				Ipc:     runtimeapi.NamespaceMode_POD,
				Network: runtimeapi.NamespaceMode_POD,
			}
			podID, podConfig = createNamespacePodSandbox(rc, namespaceOption, podSandboxName, "")

			By("create nginx container")
			prefix := "nginx-container-"
			containerName := prefix + framework.NewUUID()
			containerID, nginxContainerName, _ := createNamespaceContainer(rc, ic, podID, podConfig, containerName, nginxContainerImage, namespaceOption, nil, "")

			By("start container")
			startContainer(rc, containerID)
			Eventually(func() runtimeapi.ContainerState {
				return getContainerStatus(rc, containerID).State
			}, time.Minute, time.Second*4).Should(Equal(runtimeapi.ContainerState_CONTAINER_RUNNING))

			By("get nginx container pid")
			command := []string{"sh", "-c", "while [ ! -f /var/run/nginx.pid ]; do sleep 1; done", "&&", "cat", "/var/run/nginx.pid"}
			output := execSyncContainer(rc, containerID, command)
			nginxPid := strings.TrimSpace(string(output))
			framework.Logf("Nginx's pid is %q", nginxPid)

			By("create busybox container with hostPID")
			command = []string{"sh", "-c", "sleep 1000"}
			prefix = "container-with-HostPID-test-"
			containerName = prefix + framework.NewUUID()
			containerID, _, _ = createNamespaceContainer(rc, ic, podID, podConfig, containerName, framework.DefaultContainerImage, namespaceOption, command, "")

			By("start container")
			startContainer(rc, containerID)
			Eventually(func() runtimeapi.ContainerState {
				return getContainerStatus(rc, containerID).State
			}, time.Minute, time.Second*4).Should(Equal(runtimeapi.ContainerState_CONTAINER_RUNNING))

			By("should show its pid in the hostPID namespace container")
			cmd := []string{"pidof", "nginx", "||", "true"}
			output = execSyncContainer(rc, containerID, cmd)
			pids := strings.TrimSpace(string(output))
			framework.Logf("Got nginx's pid %q from pod %q", pids, nginxContainerName)

			if pids == "" {
				framework.Failf("nginx's pid should be seen by hostpid containers")
			}

			if !strings.Contains(pids, nginxPid) {
				framework.Failf("nginx's pid should be seen by hostpid containers")
			}

		})

		It("runtime should support HostIpc is true", func() {
			By("create shared memory segment on the host")
			out, err := exec.Command("ipcmk", "-M", "1048576").Output()
			framework.ExpectNoError(err, "failed to execute ipcmk -M 1048576")
			rawID := strings.TrimSpace(string(out))
			segmentID := strings.TrimPrefix(rawID, "Shared memory id: ")

			By("create podSandbox for security context HostIPC is true")
			namespaceOption := &runtimeapi.NamespaceOption{
				Pid:     runtimeapi.NamespaceMode_POD,
				Ipc:     runtimeapi.NamespaceMode_NODE,
				Network: runtimeapi.NamespaceMode_POD,
			}
			podID, podConfig = createNamespacePodSandbox(rc, namespaceOption, podSandboxName, "")

			By("create a default container with namespace")
			prefix := "namespace-container-"
			containerName := prefix + framework.NewUUID()
			containerID, _, _ := createNamespaceContainer(rc, ic, podID, podConfig, containerName, framework.DefaultContainerImage, namespaceOption, pauseCmd, "")

			By("start container")
			startContainer(rc, containerID)
			Eventually(func() runtimeapi.ContainerState {
				return getContainerStatus(rc, containerID).State
			}, time.Minute, time.Second*4).Should(Equal(runtimeapi.ContainerState_CONTAINER_RUNNING))

			By("check if the shared memory segment is included in the container")
			command := []string{"ipcs", "-m"}
			o := execSyncContainer(rc, containerID, command)
			Expect(o).To(ContainSubstring(segmentID), "The shared memory segment should be included in the container")
		})

		It("runtime should support HostIpc is false", func() {
			By("create shared memory segment on the host")
			out, err := exec.Command("ipcmk", "-M", "1048576").Output()
			framework.ExpectNoError(err, "failed to execute ipcmk -M 1048576")
			rawID := strings.TrimSpace(string(out))
			segmentID := strings.TrimPrefix(rawID, "Shared memory id: ")

			By("create podSandbox for security context HostIpc is false")
			namespaceOption := &runtimeapi.NamespaceOption{
				Pid:     runtimeapi.NamespaceMode_POD,
				Ipc:     runtimeapi.NamespaceMode_POD,
				Network: runtimeapi.NamespaceMode_POD,
			}
			podID, podConfig = createNamespacePodSandbox(rc, namespaceOption, podSandboxName, "")

			By("create a default container with namespace")
			prefix := "namespace-container-"
			containerName := prefix + framework.NewUUID()
			containerID, _, _ := createNamespaceContainer(rc, ic, podID, podConfig, containerName, framework.DefaultContainerImage, namespaceOption, pauseCmd, "")

			By("start container")
			startContainer(rc, containerID)
			Eventually(func() runtimeapi.ContainerState {
				return getContainerStatus(rc, containerID).State
			}, time.Minute, time.Second*4).Should(Equal(runtimeapi.ContainerState_CONTAINER_RUNNING))

			By("check if the shared memory segment is not included in the container")
			command := []string{"ipcs", "-m"}
			o := execSyncContainer(rc, containerID, command)
			Expect(o).NotTo(ContainSubstring(segmentID), "The shared memory segment should be included in the container")
		})

		It("runtime should support PodPID", func() {
			By("create podSandbox for sharing process namespace")
			namespaceOption := &runtimeapi.NamespaceOption{
				Pid: runtimeapi.NamespaceMode_POD,
			}
			framework.Logf("Pid namespace is %q", namespaceOption.Pid)
			podID, podConfig = createNamespacePodSandbox(rc, namespaceOption, podSandboxName, "")

			By("create nginx container")
			prefix := "nginx-container-"
			containerName := prefix + framework.NewUUID()
			containerID, _, _ := createNamespaceContainer(rc, ic, podID, podConfig, containerName, nginxContainerImage, namespaceOption, nil, "")

			By("start container")
			startContainer(rc, containerID)
			Eventually(func() runtimeapi.ContainerState {
				return getContainerStatus(rc, containerID).State
			}, time.Minute, time.Second*4).Should(Equal(runtimeapi.ContainerState_CONTAINER_RUNNING))

			By("get nginx container pid")
			command := []string{"cat", "/proc/1/cmdline"}
			time.Sleep(time.Second) // waits for nginx to be up-and-running
			o := execSyncContainer(rc, containerID, command)
			Expect(o).ToNot(ContainSubstring("master process"))
		})

		It("runtime should support ContainerPID", func() {
			By("create podSandbox with PID set to container")
			namespaceOption := &runtimeapi.NamespaceOption{
				Pid: runtimeapi.NamespaceMode_CONTAINER,
			}
			framework.Logf("Pid namespace is %q", namespaceOption.Pid)
			podID, podConfig = createNamespacePodSandbox(rc, namespaceOption, podSandboxName, "")

			By("create nginx container")
			prefix := "nginx-container-"
			containerName := prefix + framework.NewUUID()
			containerID, _, _ := createNamespaceContainer(rc, ic, podID, podConfig, containerName, nginxContainerImage, namespaceOption, nil, "")

			By("start container")
			startContainer(rc, containerID)
			Eventually(func() runtimeapi.ContainerState {
				return getContainerStatus(rc, containerID).State
			}, time.Minute, time.Second*4).Should(Equal(runtimeapi.ContainerState_CONTAINER_RUNNING))

			By("get nginx container pid")
			command := []string{"sh", "-c", `while ! cat /proc/1/cmdline | grep "master process"; do sleep 1; done`}
			execSyncContainer(rc, containerID, command)
		})

		It("runtime should support HostNetwork is true", func() {
			srv, err := net.Listen("tcp", ":0")
			if err != nil {
				framework.Failf("Failed to listen a tcp port: %v", err)
			}
			go func() {
				defer GinkgoRecover()
				for {
					conn, err := srv.Accept()
					if err != nil {
						return
					}
					conn.Write([]byte("hello"))
				}
			}()
			defer srv.Close()

			ports := strings.Split(srv.Addr().String(), ":")
			podID, podLogDir = createAndCheckHostNetwork(rc, ic, podSandboxName, ports[len(ports)-1], true)
		})

		It("runtime should support HostNetwork is false", func() {
			srv, err := net.Listen("tcp", ":0")
			if err != nil {
				framework.Failf("Failed to listen a tcp port: %v", err)
			}
			go func() {
				defer GinkgoRecover()
				for {
					conn, err := srv.Accept()
					if err != nil {
						return
					}
					conn.Write([]byte("hello"))
				}
			}()
			defer srv.Close()

			ports := strings.Split(srv.Addr().String(), ":")
			podID, podLogDir = createAndCheckHostNetwork(rc, ic, podSandboxName, ports[len(ports)-1], false)
		})
	})

	Context("bucket", func() {
		It("runtime should support SupplementalGroups", func() {
			By("create pod")
			podID, podConfig = framework.CreatePodSandboxForContainer(rc)

			supplementalGroups := []int64{1234, 5678}
			By("create container for security context SupplementalGroups")
			containerName := "container-with-SupplementalGroups-test-" + framework.NewUUID()
			containerConfig := &runtimeapi.ContainerConfig{
				Metadata: framework.BuildContainerMetadata(containerName, framework.DefaultAttempt),
				Image:    &runtimeapi.ImageSpec{Image: framework.DefaultContainerImage},
				Command:  pauseCmd,
				Linux: &runtimeapi.LinuxContainerConfig{
					SecurityContext: &runtimeapi.LinuxContainerSecurityContext{
						SupplementalGroups: supplementalGroups,
					},
				},
			}
			containerID := framework.CreateContainer(rc, ic, containerConfig, podID, podConfig)

			By("start container")
			startContainer(rc, containerID)
			Eventually(func() runtimeapi.ContainerState {
				return getContainerStatus(rc, containerID).State
			}, time.Minute, time.Second*4).Should(Equal(runtimeapi.ContainerState_CONTAINER_RUNNING))

			By("verify SupplementalGroups for container")
			command := []string{"id", "-G"}
			o := execSyncContainer(rc, containerID, command)
			groups := strings.Split(strings.TrimSpace(o), " ")
			Expect(groups).To(ContainElement("1234"))
			Expect(groups).To(ContainElement("5678"))
		})

		It("runtime should support RunAsUser", func() {
			By("create pod")
			podID, podConfig = framework.CreatePodSandboxForContainer(rc)

			By("create container for security context RunAsUser")
			containerID, expectedLogMessage := createRunAsUserContainer(rc, ic, podID, podConfig, "container-with-RunAsUser-test-")

			By("start container")
			startContainer(rc, containerID)
			Eventually(func() runtimeapi.ContainerState {
				return getContainerStatus(rc, containerID).State
			}, time.Minute, time.Second*4).Should(Equal(runtimeapi.ContainerState_CONTAINER_RUNNING))

			By("verify RunAsUser for container")
			command := []string{"id", "-u"}
			verifyExecSyncOutput(rc, containerID, command, expectedLogMessage)
		})

		It("runtime should support RunAsUserName", func() {
			By("create pod")
			podID, podConfig = framework.CreatePodSandboxForContainer(rc)

			By("create container for security context RunAsUser")
			containerID, expectedLogMessage := createRunAsUserNameContainer(rc, ic, podID, podConfig, "container-with-RunAsUserName-test-")

			By("start container")
			startContainer(rc, containerID)
			Eventually(func() runtimeapi.ContainerState {
				return getContainerStatus(rc, containerID).State
			}, time.Minute, time.Second*4).Should(Equal(runtimeapi.ContainerState_CONTAINER_RUNNING))

			By("verify RunAsUserName for container")
			command := []string{"id", "-nu"}
			verifyExecSyncOutput(rc, containerID, command, expectedLogMessage)
		})

		It("runtime should support RunAsGroup", func() {
			By("create pod")
			podID, podConfig, podLogDir = createPodSandboxWithLogDirectory(rc)

			By("create container for security context RunAsGroup")
			containerName := "container-with-RunAsGroup-test-" + framework.NewUUID()
			containerID, expectedLogMessage := createRunAsGroupContainer(rc, ic, podID, podConfig, containerName)

			By("start container")
			startContainer(rc, containerID)
			Eventually(func() runtimeapi.ContainerState {
				return getContainerStatus(rc, containerID).State
			}, time.Minute, time.Second*4).Should(Equal(runtimeapi.ContainerState_CONTAINER_EXITED))

			By("verify RunAsGroup for container")
			matchContainerOutput(podConfig, containerName, expectedLogMessage)
		})

		It("runtime should return error if RunAsGroup is set without RunAsUser", func() {
			By("create pod")
			podID, podConfig = framework.CreatePodSandboxForContainer(rc)

			By("create container with invalid RunAsGroup")
			containerName := "container-with-RunAsGroup-without-RunAsUser-test-" + framework.NewUUID()
			createInvalidRunAsGroupContainer(rc, ic, podID, podConfig, containerName)
		})

		It("runtime should support that ReadOnlyRootfs is false", func() {
			By("create pod with log")
			podID, podConfig, podLogDir = createPodSandboxWithLogDirectory(rc)

			By("create container with ReadOnlyRootfs_false")
			readOnlyRootfs := false
			logPath, containerID := createReadOnlyRootfsContainer(rc, ic, podID, podConfig, "container-with-ReadOnlyRootfs-false-test-", readOnlyRootfs)

			By("start container")
			startContainer(rc, containerID)
			Eventually(func() runtimeapi.ContainerState {
				return getContainerStatus(rc, containerID).State
			}, time.Minute, time.Second*4).Should(Equal(runtimeapi.ContainerState_CONTAINER_EXITED))

			By("Check whether rootfs is writable")
			checkRootfs(podConfig, logPath, readOnlyRootfs)
		})

		It("runtime should support that ReadOnlyRootfs is true", func() {
			By("create pod with log")
			podID, podConfig, podLogDir = createPodSandboxWithLogDirectory(rc)

			By("create container with ReadOnlyRootfs_true")
			readOnlyRootfs := true
			logPath, containerID := createReadOnlyRootfsContainer(rc, ic, podID, podConfig, "container-with-ReadOnlyRootfs-true-test-", readOnlyRootfs)

			By("start container")
			startContainer(rc, containerID)
			Eventually(func() runtimeapi.ContainerState {
				return getContainerStatus(rc, containerID).State
			}, time.Minute, time.Second*4).Should(Equal(runtimeapi.ContainerState_CONTAINER_EXITED))

			By("Check whether rootfs is read-only")
			checkRootfs(podConfig, logPath, readOnlyRootfs)
		})

		It("runtime should support Privileged is true", func() {
			By("create pod")
			isPrivileged := true
			podID, podConfig = createPrivilegedPodSandbox(rc, isPrivileged)

			By("create container for security context Privileged is true")
			containerID := createPrivilegedContainer(rc, ic, podID, podConfig, "container-with-isPrivileged-test-", isPrivileged)

			By("start container")
			startContainer(rc, containerID)
			Eventually(func() runtimeapi.ContainerState {
				return getContainerStatus(rc, containerID).State
			}, time.Minute, time.Second*4).Should(Equal(runtimeapi.ContainerState_CONTAINER_RUNNING))

			By("check the Privileged container")
			checkNetworkManagement(rc, containerID, isPrivileged)
		})

		It("runtime should support Privileged is false", func() {
			By("create pod")
			notPrivileged := false
			podID, podConfig = createPrivilegedPodSandbox(rc, notPrivileged)

			By("create container for security context Privileged is true")
			containerID := createPrivilegedContainer(rc, ic, podID, podConfig, "container-with-notPrivileged-test-", notPrivileged)

			By("start container")
			startContainer(rc, containerID)
			Eventually(func() runtimeapi.ContainerState {
				return getContainerStatus(rc, containerID).State
			}, time.Minute, time.Second*4).Should(Equal(runtimeapi.ContainerState_CONTAINER_RUNNING))

			By("check the Privileged container")
			checkNetworkManagement(rc, containerID, notPrivileged)
		})

		It("runtime should support setting Capability", func() {
			By("create pod")
			podID, podConfig = framework.CreatePodSandboxForContainer(rc)

			By("create container with security context Capability and test")
			containerID := createCapabilityContainer(rc, ic, podID, podConfig, "container-with-Capability-test-")

			startContainer(rc, containerID)
			Eventually(func() runtimeapi.ContainerState {
				return getContainerStatus(rc, containerID).State
			}, time.Minute, time.Second*4).Should(Equal(runtimeapi.ContainerState_CONTAINER_RUNNING))

			checkNetworkManagement(rc, containerID, true)

			By("create container without security context Capability and test")
			containerID = framework.CreateDefaultContainer(rc, ic, podID, podConfig, "container-with-notCapability-test-")

			startContainer(rc, containerID)
			Eventually(func() runtimeapi.ContainerState {
				return getContainerStatus(rc, containerID).State
			}, time.Minute, time.Second*4).Should(Equal(runtimeapi.ContainerState_CONTAINER_RUNNING))

			checkNetworkManagement(rc, containerID, false)
		})

		It("runtime should support MaskedPaths", func() {
			By("create pod")
			podID, podConfig = framework.CreatePodSandboxForContainer(rc)

			By("create container with MaskedPaths")
			containerName := "container-with-maskedpaths" + framework.NewUUID()
			containerConfig := &runtimeapi.ContainerConfig{
				Metadata: framework.BuildContainerMetadata(containerName, framework.DefaultAttempt),
				Image:    &runtimeapi.ImageSpec{Image: framework.DefaultContainerImage},
				Command:  pauseCmd,
				Linux: &runtimeapi.LinuxContainerConfig{
					SecurityContext: &runtimeapi.LinuxContainerSecurityContext{
						MaskedPaths: []string{"/bin/ls"},
					},
				},
			}

			containerID := framework.CreateContainer(rc, ic, containerConfig, podID, podConfig)
			startContainer(rc, containerID)
			Eventually(func() runtimeapi.ContainerState {
				return getContainerStatus(rc, containerID).State
			}, time.Minute, time.Second*4).Should(Equal(runtimeapi.ContainerState_CONTAINER_RUNNING))

			cmd := []string{"/bin/sh", "-c", "ls"}
			_, stderr, err := rc.ExecSync(containerID, cmd, time.Duration(defaultExecSyncTimeout)*time.Second)
			Expect(err).To(HaveOccurred())
			Expect(string(stderr)).To(Equal("/bin/sh: ls: Permission denied\n"))
		})

		It("runtime should support ReadonlyPaths", func() {
			By("create pod")
			podID, podConfig = framework.CreatePodSandboxForContainer(rc)

			By("create container with ReadonlyPaths")
			containerName := "container-with-readonlypaths" + framework.NewUUID()
			containerConfig := &runtimeapi.ContainerConfig{
				Metadata: framework.BuildContainerMetadata(containerName, framework.DefaultAttempt),
				Image:    &runtimeapi.ImageSpec{Image: framework.DefaultContainerImage},
				Command:  pauseCmd,
				Linux: &runtimeapi.LinuxContainerConfig{
					SecurityContext: &runtimeapi.LinuxContainerSecurityContext{
						ReadonlyPaths: []string{"/tmp"},
					},
				},
			}

			containerID := framework.CreateContainer(rc, ic, containerConfig, podID, podConfig)
			startContainer(rc, containerID)
			Eventually(func() runtimeapi.ContainerState {
				return getContainerStatus(rc, containerID).State
			}, time.Minute, time.Second*4).Should(Equal(runtimeapi.ContainerState_CONTAINER_RUNNING))

			cmd := []string{"touch", "/tmp/test"}
			_, stderr, err := rc.ExecSync(containerID, cmd, time.Duration(defaultExecSyncTimeout)*time.Second)
			Expect(err).To(HaveOccurred())
			Expect(string(stderr)).To(Equal("touch: /tmp/test: Read-only file system\n"))
		})
	})

	// TODO(random-liu): We should set apparmor to unconfined in seccomp test to prevent
	// them from interfering with each other.
	Context("SeccompProfilePath", func() {
		const (
			// profile which denies sethostname syscall
			seccompBlockHostNameProfile = `{
     "defaultAction": "SCMP_ACT_ALLOW",
     "syscalls": [
         {
             "names": ["sethostname"],
             "action": "SCMP_ACT_ERRNO"
         }
     ]
}`
			// profile which denies chmod syscall
			seccompBlockChmodProfile = `{
     "defaultAction": "SCMP_ACT_ALLOW",
     "syscalls": [
         {
             "names": ["chmod"],
             "action": "SCMP_ACT_ERRNO"
         }
     ]
}`
		)
		var profileDir, blockHostNameProfilePath, blockchmodProfilePath string
		var err error
		sysAdminCap := []string{"SYS_ADMIN"}

		BeforeEach(func() {
			profileDir, err = createSeccompProfileDir()
			if err != nil {
				Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Failed creating seccomp profile directory: %v", err))
				return
			}
			dirToCleanup = append(dirToCleanup, profileDir)
			blockHostNameProfilePath, err = createSeccompProfile(seccompBlockHostNameProfile, "block-host-name.json", profileDir)
			if err != nil {
				Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Failed creating seccomp block hostname profile: %v", err))
				return
			}
			blockchmodProfilePath, err = createSeccompProfile(seccompBlockChmodProfile, "block-chmod.json", profileDir)
			if err != nil {
				Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Failed creating seccomp block chmod profile: %v", err))
				return
			}
		})

		It("should support seccomp unconfined on the container", func() {
			var containerID string

			By("create seccomp sandbox and container")
			podID, containerID = seccompTestContainer(
				rc, ic,
				&runtimeapi.SecurityProfile{
					ProfileType: runtimeapi.SecurityProfile_Unconfined,
				},
			)

			By("verify seccomp profile")
			verifySeccomp(rc, containerID, []string{"grep", "ecc", "/proc/self/status"}, false, "0") // seccomp disabled
		})

		It("should support seccomp localhost profile on the container", func() {
			var containerID string

			By("create seccomp sandbox and container")
			podID, containerID = seccompTestContainer(
				rc, ic,
				&runtimeapi.SecurityProfile{
					ProfileType:  runtimeapi.SecurityProfile_Localhost,
					LocalhostRef: blockchmodProfilePath,
				},
			)

			By("verify seccomp profile")
			verifySeccomp(rc, containerID, []string{"chmod", "400", "/"}, true, "Operation not permitted") // seccomp denied
		})

		// SYS_ADMIN capability allows sethostname, and seccomp is unconfined. sethostname should work.
		It("runtime should not block setting host name with unconfined seccomp and SYS_ADMIN", func() {
			privileged := false
			expectContainerCreateToPass := true
			By("create pod")
			podID, podConfig = framework.CreatePodSandboxForContainer(rc)
			By("create container with seccompBlockHostNameProfile and test")
			containerID := createSeccompContainer(rc, ic, podID, podConfig,
				"container-with-block-hostname-seccomp-profile-test-",
				&runtimeapi.SecurityProfile{
					ProfileType: runtimeapi.SecurityProfile_Unconfined,
				},
				sysAdminCap, privileged, expectContainerCreateToPass)
			startContainer(rc, containerID)
			Eventually(func() runtimeapi.ContainerState {
				return getContainerStatus(rc, containerID).State
			}, time.Minute, time.Second*4).Should(Equal(runtimeapi.ContainerState_CONTAINER_RUNNING))
			checkSetHostname(rc, containerID, true)
		})

		// SYS_ADMIN capability allows sethostname, but seccomp profile should be able to block it.
		It("runtime should support an seccomp profile that blocks setting hostname with SYS_ADMIN", func() {
			privileged := false
			expectContainerCreateToPass := true
			By("create pod")
			podID, podConfig = framework.CreatePodSandboxForContainer(rc)
			By("create container with seccompBlockHostNameProfile and test")
			containerID := createSeccompContainer(rc, ic, podID, podConfig,
				"container-with-block-hostname-seccomp-profile-test-",
				&runtimeapi.SecurityProfile{
					ProfileType:  runtimeapi.SecurityProfile_Localhost,
					LocalhostRef: blockHostNameProfilePath,
				},
				sysAdminCap, privileged, expectContainerCreateToPass)
			startContainer(rc, containerID)
			Eventually(func() runtimeapi.ContainerState {
				return getContainerStatus(rc, containerID).State
			}, time.Minute, time.Second*4).Should(Equal(runtimeapi.ContainerState_CONTAINER_RUNNING))
			checkSetHostname(rc, containerID, false)
		})

		It("runtime should ignore a seccomp profile that blocks setting hostname when privileged", func() {
			privileged := true
			expectContainerCreateToPass := true
			By("create privileged pod")
			podID, podConfig = createPrivilegedPodSandbox(rc, true)
			By("create privileged container with seccompBlockHostNameProfile and test")
			containerID := createSeccompContainer(rc, ic, podID, podConfig,
				"container-with-block-hostname-seccomp-profile-test-",
				&runtimeapi.SecurityProfile{
					ProfileType:  runtimeapi.SecurityProfile_Localhost,
					LocalhostRef: blockHostNameProfilePath,
				},
				nil, privileged, expectContainerCreateToPass)
			startContainer(rc, containerID)
			Eventually(func() runtimeapi.ContainerState {
				return getContainerStatus(rc, containerID).State
			}, time.Minute, time.Second*4).Should(Equal(runtimeapi.ContainerState_CONTAINER_RUNNING))
			checkSetHostname(rc, containerID, true)
		})

		Context("RuntimeDefault", func() {
			It("should support seccomp RuntimeDefault on the container", func() {
				var containerID string

				By("create seccomp sandbox and container")
				podID, containerID = seccompTestContainer(rc, ic,
					&runtimeapi.SecurityProfile{
						ProfileType: runtimeapi.SecurityProfile_RuntimeDefault,
					},
				)

				By("verify seccomp profile")
				verifySeccomp(rc, containerID, []string{"grep", "ecc", "/proc/self/status"}, false, "2") // seccomp filtered
			})

			It("runtime should support setting hostname with RuntimeDefault seccomp profile and SYS_ADMIN", func() {
				privileged := false
				expectContainerCreateToPass := true
				By("create pod")
				podID, podConfig = framework.CreatePodSandboxForContainer(rc)
				By("create container with RuntimeDefault seccomp profile and test")
				containerID := createSeccompContainer(rc, ic, podID, podConfig,
					"container-with-runtimedefault-seccomp-profile-test-",
					&runtimeapi.SecurityProfile{
						ProfileType: runtimeapi.SecurityProfile_RuntimeDefault,
					},
					sysAdminCap, privileged, expectContainerCreateToPass)
				startContainer(rc, containerID)
				Eventually(func() runtimeapi.ContainerState {
					return getContainerStatus(rc, containerID).State
				}, time.Minute, time.Second*4).Should(Equal(runtimeapi.ContainerState_CONTAINER_RUNNING))
				checkSetHostname(rc, containerID, true)
			})

			It("runtime should block sethostname with RuntimeDefault seccomp profile and no extra caps", func() {
				privileged := false
				expectContainerCreateToPass := true
				By("create pod")
				podID, podConfig = framework.CreatePodSandboxForContainer(rc)
				By("create container with RuntimeDefault seccomp profile and test")
				containerID := createSeccompContainer(rc, ic, podID, podConfig,
					"container-with-runtimedefault-seccomp-profile-test-",
					&runtimeapi.SecurityProfile{
						ProfileType: runtimeapi.SecurityProfile_RuntimeDefault,
					},
					nil, privileged, expectContainerCreateToPass)
				startContainer(rc, containerID)
				Eventually(func() runtimeapi.ContainerState {
					return getContainerStatus(rc, containerID).State
				}, time.Minute, time.Second*4).Should(Equal(runtimeapi.ContainerState_CONTAINER_RUNNING))
				checkSetHostname(rc, containerID, false)
			})
		})
	})

	Context("NoNewPrivs", func() {
		BeforeEach(func() {
			podID, podConfig, podLogDir = createPodSandboxWithLogDirectory(rc)
		})

		createContainerWithNoNewPrivs := func(name string, noNewPrivs bool, uid int64) string {
			By(fmt.Sprintf("create container %s", name))
			containerConfig := &runtimeapi.ContainerConfig{
				Metadata: framework.BuildContainerMetadata(name, framework.DefaultAttempt),
				Image:    &runtimeapi.ImageSpec{Image: noNewPrivsImage},
				Linux: &runtimeapi.LinuxContainerConfig{
					SecurityContext: &runtimeapi.LinuxContainerSecurityContext{
						NoNewPrivs: noNewPrivs,
						RunAsUser: &runtimeapi.Int64Value{
							Value: uid,
						},
					},
				},
				LogPath: fmt.Sprintf("%s.log", name),
			}
			containerID := framework.CreateContainer(rc, ic, containerConfig, podID, podConfig)

			// wait container started and check the status.
			startContainer(rc, containerID)
			Eventually(func() runtimeapi.ContainerState {
				return getContainerStatus(rc, containerID).State
			}, time.Minute, time.Second*4).Should(Equal(runtimeapi.ContainerState_CONTAINER_EXITED))

			return containerID
		}
		It("should not allow privilege escalation when true", func() {
			containerName := "alpine-nnp-true-" + string(framework.NewUUID())
			createContainerWithNoNewPrivs(containerName, true, 1000)
			matchContainerOutput(podConfig, containerName, "Effective uid: 1000\n")
		})

		It("should allow privilege escalation when false", func() {
			containerName := "alpine-nnp-false-" + string(framework.NewUUID())
			createContainerWithNoNewPrivs(containerName, false, 1000)
			matchContainerOutput(podConfig, containerName, "Effective uid: 0\n")
		})
	})
})

// matchContainerOutput matches log line in container logs.
func matchContainerOutput(podConfig *runtimeapi.PodSandboxConfig, name, output string) {
	By("check container output")
	verifyLogContents(podConfig, fmt.Sprintf("%s.log", name), output, stdoutType)
}

// createRunAsUserContainer creates the container with specified RunAsUser in ContainerConfig.
func createRunAsUserContainer(rc internalapi.RuntimeService, ic internalapi.ImageManagerService, podID string, podConfig *runtimeapi.PodSandboxConfig, prefix string) (string, string) {
	By("create RunAsUser container")
	var uidV runtimeapi.Int64Value
	uidV.Value = 1001
	expectedLogMessage := "1001\n"

	By("create a container with RunAsUser")
	containerName := prefix + framework.NewUUID()
	containerConfig := &runtimeapi.ContainerConfig{
		Metadata: framework.BuildContainerMetadata(containerName, framework.DefaultAttempt),
		Image:    &runtimeapi.ImageSpec{Image: framework.DefaultContainerImage},
		Command:  pauseCmd,
		Linux: &runtimeapi.LinuxContainerConfig{
			SecurityContext: &runtimeapi.LinuxContainerSecurityContext{
				RunAsUser: &uidV,
			},
		},
	}

	return framework.CreateContainer(rc, ic, containerConfig, podID, podConfig), expectedLogMessage
}

// createRunAsUserNameContainer creates the container with specified RunAsUserName in ContainerConfig.
func createRunAsUserNameContainer(rc internalapi.RuntimeService, ic internalapi.ImageManagerService, podID string, podConfig *runtimeapi.PodSandboxConfig, prefix string) (string, string) {
	By("create RunAsUserName container")
	userName := "nobody"
	expectedLogMessage := userName + "\n"

	By("create a container with RunAsUserName")
	containerName := prefix + framework.NewUUID()
	containerConfig := &runtimeapi.ContainerConfig{
		Metadata: framework.BuildContainerMetadata(containerName, framework.DefaultAttempt),
		Image:    &runtimeapi.ImageSpec{Image: framework.DefaultContainerImage},
		Command:  pauseCmd,
		Linux: &runtimeapi.LinuxContainerConfig{
			SecurityContext: &runtimeapi.LinuxContainerSecurityContext{
				RunAsUsername: userName,
			},
		},
	}
	return framework.CreateContainer(rc, ic, containerConfig, podID, podConfig), expectedLogMessage
}

// createRunAsGroupContainer creates the container with specified RunAsGroup in ContainerConfig.
func createRunAsGroupContainer(rc internalapi.RuntimeService, ic internalapi.ImageManagerService, podID string, podConfig *runtimeapi.PodSandboxConfig, containerName string) (string, string) {
	By("create RunAsGroup container")
	var uidV, gidV runtimeapi.Int64Value
	uidV.Value = 1001
	gidV.Value = 1002
	expectedLogMessage := "1001:1002\n"

	By("create a container with RunAsUser and RunAsGroup")
	containerConfig := &runtimeapi.ContainerConfig{
		Metadata: framework.BuildContainerMetadata(containerName, framework.DefaultAttempt),
		Image:    &runtimeapi.ImageSpec{Image: framework.DefaultContainerImage},
		Command:  []string{"sh", "-c", "echo $(id -u):$(id -g)"},
		Linux: &runtimeapi.LinuxContainerConfig{
			SecurityContext: &runtimeapi.LinuxContainerSecurityContext{
				RunAsUser:  &uidV,
				RunAsGroup: &gidV,
			},
		},
		LogPath: fmt.Sprintf("%s.log", containerName),
	}
	return framework.CreateContainer(rc, ic, containerConfig, podID, podConfig), expectedLogMessage
}

// createInvalidRunAsGroupContainer creates the container with specified RunAsGroup without
// RunAsUser specified in ContainerConfig.
func createInvalidRunAsGroupContainer(rc internalapi.RuntimeService, ic internalapi.ImageManagerService, podID string, podConfig *runtimeapi.PodSandboxConfig, containerName string) {
	By("create invalid RunAsGroup container")
	var gidV runtimeapi.Int64Value
	gidV.Value = 1002

	By("create a container with RunAsGroup without RunAsUser")
	containerConfig := &runtimeapi.ContainerConfig{
		Metadata: framework.BuildContainerMetadata(containerName, framework.DefaultAttempt),
		Image:    &runtimeapi.ImageSpec{Image: framework.DefaultContainerImage},
		Command:  []string{"sh", "-c", "echo $(id -u):$(id -g)"},
		Linux: &runtimeapi.LinuxContainerConfig{
			SecurityContext: &runtimeapi.LinuxContainerSecurityContext{
				RunAsGroup: &gidV,
			},
		},
	}
	_, err := framework.CreateContainerWithError(rc, ic, containerConfig, podID, podConfig)
	Expect(err).To(HaveOccurred())
}

// createNamespacePodSandbox creates a PodSandbox with different NamespaceOption config for creating containers.
func createNamespacePodSandbox(rc internalapi.RuntimeService, podSandboxNamespace *runtimeapi.NamespaceOption, podSandboxName string, podLogPath string) (string, *runtimeapi.PodSandboxConfig) {
	By("create NamespaceOption podSandbox")
	uid := framework.DefaultUIDPrefix + framework.NewUUID()
	namespace := framework.DefaultNamespacePrefix + framework.NewUUID()
	config := &runtimeapi.PodSandboxConfig{
		Metadata: framework.BuildPodSandboxMetadata(podSandboxName, uid, namespace, framework.DefaultAttempt),
		Linux: &runtimeapi.LinuxPodSandboxConfig{
			SecurityContext: &runtimeapi.LinuxSandboxSecurityContext{
				NamespaceOptions: podSandboxNamespace,
			},
		},
		LogDirectory: podLogPath,
		Labels:       framework.DefaultPodLabels,
	}

	return framework.RunPodSandbox(rc, config), config
}

// createNamespaceContainer creates container with different NamespaceOption config.
func createNamespaceContainer(rc internalapi.RuntimeService, ic internalapi.ImageManagerService, podID string, podConfig *runtimeapi.PodSandboxConfig, containerName string, image string, containerNamespace *runtimeapi.NamespaceOption, command []string, path string) (string, string, string) {
	By("create NamespaceOption container")
	containerConfig := &runtimeapi.ContainerConfig{
		Metadata: framework.BuildContainerMetadata(containerName, framework.DefaultAttempt),
		Image:    &runtimeapi.ImageSpec{Image: image},
		Command:  command,
		Linux: &runtimeapi.LinuxContainerConfig{
			SecurityContext: &runtimeapi.LinuxContainerSecurityContext{
				NamespaceOptions: containerNamespace,
			},
		},
		LogPath: path,
	}

	return framework.CreateContainer(rc, ic, containerConfig, podID, podConfig), containerName, containerConfig.LogPath

}

// createReadOnlyRootfsContainer creates the container with specified ReadOnlyRootfs in ContainerConfig.
func createReadOnlyRootfsContainer(rc internalapi.RuntimeService, ic internalapi.ImageManagerService, podID string, podConfig *runtimeapi.PodSandboxConfig, prefix string, readonly bool) (string, string) {
	By("create ReadOnlyRootfs container")
	containerName := prefix + framework.NewUUID()
	path := fmt.Sprintf("%s.log", containerName)
	containerConfig := &runtimeapi.ContainerConfig{
		Metadata: framework.BuildContainerMetadata(containerName, framework.DefaultAttempt),
		Image:    &runtimeapi.ImageSpec{Image: framework.DefaultContainerImage},
		Command:  []string{"sh", "-c", "touch test.go && [ -f test.go ] && echo 'Found'"},
		Linux: &runtimeapi.LinuxContainerConfig{
			SecurityContext: &runtimeapi.LinuxContainerSecurityContext{
				ReadonlyRootfs: readonly,
			},
		},
		LogPath: path,
	}

	return containerConfig.LogPath, framework.CreateContainer(rc, ic, containerConfig, podID, podConfig)
}

// checkRootfs checks whether the rootfs parameter of the ContainerConfig is working properly.
func checkRootfs(podConfig *runtimeapi.PodSandboxConfig, logpath string, readOnlyRootfs bool) {
	if readOnlyRootfs {
		failLog := "touch: test.go: Read-only file system"
		expectedLogMessage := failLog + "\n"
		verifyLogContents(podConfig, logpath, expectedLogMessage, stderrType)
	} else {
		successLog := "Found"
		expectedLogMessage := successLog + "\n"
		verifyLogContents(podConfig, logpath, expectedLogMessage, stdoutType)
	}
}

// createPrivilegedPodSandbox creates a PodSandbox with Privileged of SecurityContext config.
func createPrivilegedPodSandbox(rc internalapi.RuntimeService, privileged bool) (string, *runtimeapi.PodSandboxConfig) {
	By("create Privileged podSandbox")
	podSandboxName := "create-Privileged-PodSandbox-for-container-" + framework.NewUUID()
	uid := framework.DefaultUIDPrefix + framework.NewUUID()
	namespace := framework.DefaultNamespacePrefix + framework.NewUUID()
	config := &runtimeapi.PodSandboxConfig{
		Metadata: framework.BuildPodSandboxMetadata(podSandboxName, uid, namespace, framework.DefaultAttempt),
		Linux: &runtimeapi.LinuxPodSandboxConfig{
			SecurityContext: &runtimeapi.LinuxSandboxSecurityContext{
				Privileged: privileged,
			},
		},
		Labels: framework.DefaultPodLabels,
	}

	return framework.RunPodSandbox(rc, config), config
}

// createPrivilegedContainer creates container with specified Privileged in ContainerConfig.
func createPrivilegedContainer(rc internalapi.RuntimeService, ic internalapi.ImageManagerService, podID string, podConfig *runtimeapi.PodSandboxConfig, prefix string, privileged bool) string {
	By("create Privileged container")
	containerName := prefix + framework.NewUUID()
	containerConfig := &runtimeapi.ContainerConfig{
		Metadata: framework.BuildContainerMetadata(containerName, framework.DefaultAttempt),
		Image:    &runtimeapi.ImageSpec{Image: framework.DefaultContainerImage},
		Command:  pauseCmd,
		Linux: &runtimeapi.LinuxContainerConfig{
			SecurityContext: &runtimeapi.LinuxContainerSecurityContext{
				Privileged: privileged,
			},
		},
	}

	return framework.CreateContainer(rc, ic, containerConfig, podID, podConfig)
}

// checkNetworkManagement checks the container's network management works fine.
func checkNetworkManagement(rc internalapi.RuntimeService, containerID string, manageable bool) {
	cmd := []string{"brctl", "addbr", "foobar"}

	stdout, stderr, err := rc.ExecSync(containerID, cmd, time.Duration(defaultExecSyncTimeout)*time.Second)
	msg := fmt.Sprintf("cmd %v, stdout %q, stderr %q", cmd, stdout, stderr)

	if manageable {
		Expect(err).NotTo(HaveOccurred(), msg)
	} else {
		Expect(err).To(HaveOccurred(), msg)
	}
}

// createCapabilityContainer creates container with specified Capability in ContainerConfig.
func createCapabilityContainer(rc internalapi.RuntimeService, ic internalapi.ImageManagerService, podID string, podConfig *runtimeapi.PodSandboxConfig, prefix string) string {
	By("create Capability container")
	containerName := prefix + framework.NewUUID()
	containerConfig := &runtimeapi.ContainerConfig{
		Metadata: framework.BuildContainerMetadata(containerName, framework.DefaultAttempt),
		Image:    &runtimeapi.ImageSpec{Image: framework.DefaultContainerImage},
		Command:  pauseCmd,
		Linux: &runtimeapi.LinuxContainerConfig{
			SecurityContext: &runtimeapi.LinuxContainerSecurityContext{
				Capabilities: &runtimeapi.Capability{
					AddCapabilities: []string{"NET_ADMIN"},
				},
			},
		},
	}

	return framework.CreateContainer(rc, ic, containerConfig, podID, podConfig)
}

func createAndCheckHostNetwork(rc internalapi.RuntimeService, ic internalapi.ImageManagerService, podSandboxName, hostNetworkPort string, hostNetwork bool) (podID, podLogDir string) {
	By(fmt.Sprintf("creating a podSandbox with hostNetwork %v", hostNetwork))
	netNSMode := runtimeapi.NamespaceMode_POD
	if hostNetwork {
		netNSMode = runtimeapi.NamespaceMode_NODE
	}
	namespaceOptions := &runtimeapi.NamespaceOption{
		Pid:     runtimeapi.NamespaceMode_POD,
		Ipc:     runtimeapi.NamespaceMode_POD,
		Network: netNSMode,
	}
	podLogDir, podLogPath := createLogTempDir(podSandboxName)
	podID, podConfig := createNamespacePodSandbox(rc, namespaceOptions, podSandboxName, podLogPath)

	By("create a container in the sandbox")
	command := []string{"sh", "-c", "netstat -ln"}
	containerName := "container-with-HostNetwork-test-" + framework.NewUUID()
	path := fmt.Sprintf("%s.log", containerName)
	containerID, _, logPath := createNamespaceContainer(rc, ic, podID, podConfig, containerName, framework.DefaultContainerImage, namespaceOptions, command, path)

	By("start container")
	startContainer(rc, containerID)

	By("checking host http service port in the container")
	Eventually(func() error {
		log := parseLogLine(podConfig, logPath)
		for _, msg := range log {
			if strings.Contains(string(msg.log), fmt.Sprintf(":%s", hostNetworkPort)) {
				if hostNetwork {
					return nil
				}

				return fmt.Errorf("host port %s should not in container's port list", hostNetworkPort)
			}
		}

		if hostNetwork {
			return fmt.Errorf("host port %s should be in container's port list", hostNetworkPort)
		}
		return nil
	}, time.Minute, time.Second).Should(BeNil())

	return podID, podLogDir
}

// createSeccompProfileDir creates a seccomp test profile directory.
func createSeccompProfileDir() (string, error) {
	hostPath, err := ioutil.TempDir("", "seccomp-tests")
	if err != nil {
		return "", errors.Wrapf(err, "create tempdir %q", hostPath)
	}
	return hostPath, nil
}

// createSeccompProfile creates a seccomp test profile with profileContents.
func createSeccompProfile(profileContents string, profileName string, hostPath string) (string, error) {
	profilePath := filepath.Join(hostPath, profileName)
	err := ioutil.WriteFile(profilePath, []byte(profileContents), 0644)
	if err != nil {
		return "", errors.Wrapf(err, "create %s", profilePath)
	}
	return profilePath, nil
}

// seccompTestContainer creates and starts a seccomp sandbox and a container.
func seccompTestContainer(rc internalapi.RuntimeService, ic internalapi.ImageManagerService, seccomp *runtimeapi.SecurityProfile) (string, string) {
	By("create seccomp sandbox")
	podSandboxName := "seccomp-sandbox-" + framework.NewUUID()
	uid := framework.DefaultUIDPrefix + framework.NewUUID()
	namespace := framework.DefaultNamespacePrefix + framework.NewUUID()
	podConfig := &runtimeapi.PodSandboxConfig{
		Metadata: framework.BuildPodSandboxMetadata(podSandboxName, uid, namespace, framework.DefaultAttempt),
		Linux: &runtimeapi.LinuxPodSandboxConfig{
			SecurityContext: &runtimeapi.LinuxSandboxSecurityContext{
				Seccomp: seccomp,
			},
		},
		Labels: framework.DefaultPodLabels,
	}
	podID := framework.RunPodSandbox(rc, podConfig)

	By("create container")
	containerNamePrefix := fmt.Sprintf(
		"seccomp-container-%s-%s",
		strings.ToLower(seccomp.GetProfileType().String()),
		framework.NewUUID(),
	)
	containerName := containerNamePrefix + framework.NewUUID()
	containerConfig := &runtimeapi.ContainerConfig{
		Metadata: framework.BuildContainerMetadata(containerName, framework.DefaultAttempt),
		Image:    &runtimeapi.ImageSpec{Image: framework.DefaultContainerImage},
		Command:  pauseCmd,
		Linux: &runtimeapi.LinuxContainerConfig{
			SecurityContext: &runtimeapi.LinuxContainerSecurityContext{
				Seccomp: seccomp,
			},
		},
	}
	containerID := framework.CreateContainer(rc, ic, containerConfig, podID, podConfig)

	By("start container")
	startContainer(rc, containerID)
	Eventually(func() runtimeapi.ContainerState {
		return getContainerStatus(rc, containerID).State
	}, time.Minute, time.Second*4).Should(Equal(runtimeapi.ContainerState_CONTAINER_RUNNING))

	return podID, containerID
}

func verifySeccomp(rc internalapi.RuntimeService, containerID string, command []string, expectError bool, output string) {
	stdout, stderr, err := rc.ExecSync(containerID, command, time.Duration(defaultExecSyncTimeout)*time.Second)
	msg := fmt.Sprintf("cmd %v, stdout %q, stderr %q, with err: %v", command, stdout, stderr, err)

	if expectError {
		Expect(err).To(HaveOccurred(), msg)
		Expect(string(stderr)).To(ContainSubstring(output))
	} else {
		Expect(err).NotTo(HaveOccurred(), msg)
		Expect(string(stdout)).To(ContainSubstring(output))
	}
}

// createSeccompContainer creates container with the specified seccomp profile.
func createSeccompContainer(rc internalapi.RuntimeService,
	ic internalapi.ImageManagerService,
	podID string,
	podConfig *runtimeapi.PodSandboxConfig,
	prefix string,
	profile *runtimeapi.SecurityProfile,
	caps []string,
	privileged bool,
	expectContainerCreateToPass bool) string {
	By("create " + profile.GetProfileType().String() + " Seccomp container")
	containerName := prefix + framework.NewUUID()
	containerConfig := &runtimeapi.ContainerConfig{
		Metadata: framework.BuildContainerMetadata(containerName, framework.DefaultAttempt),
		Image:    &runtimeapi.ImageSpec{Image: framework.DefaultContainerImage},
		Command:  []string{"sleep", "60"},
		Linux: &runtimeapi.LinuxContainerConfig{
			SecurityContext: &runtimeapi.LinuxContainerSecurityContext{
				Privileged: privileged,
				Capabilities: &runtimeapi.Capability{
					AddCapabilities: caps,
				},
				Seccomp: profile,
			},
		},
	}

	return createContainerWithExpectation(rc, ic, containerConfig, podID, podConfig, expectContainerCreateToPass)
}

// createContainerWithExpectation creates a container with the prefix of containerName
// and expectation of failure or success, depending on parameter, in the create step.
func createContainerWithExpectation(rc internalapi.RuntimeService,
	ic internalapi.ImageManagerService,
	config *runtimeapi.ContainerConfig,
	podID string,
	podConfig *runtimeapi.PodSandboxConfig,
	expectContainerCreateToPass bool) string {
	// Pull the image if it does not exist. (don't fail for inability to pull image)
	imageName := config.Image.Image
	if !strings.Contains(imageName, ":") {
		imageName = imageName + ":latest"
	}
	status := framework.ImageStatus(ic, imageName)
	if status == nil {
		framework.PullPublicImage(ic, imageName, nil)
	}
	By("Create container.")
	containerID, err := rc.CreateContainer(podID, config, podConfig)

	if !expectContainerCreateToPass {
		msg := fmt.Sprintf("create should fail with err %v", err)
		Expect(err).To(HaveOccurred(), msg)
	} else {
		framework.ExpectNoError(err, "failed to create container: %v", err)
		framework.Logf("Created container %q\n", containerID)
	}
	return containerID
}

// checkSetHostname checks if the hostname can be set in the container.
func checkSetHostname(rc internalapi.RuntimeService, containerID string, setable bool) {
	By("set hostname in container to determine whether sethostname is blocked")

	cmd := []string{"hostname", "ANewHostName"}
	stdout, stderr, err := rc.ExecSync(containerID, cmd, time.Duration(defaultExecSyncTimeout)*time.Second)
	msg := fmt.Sprintf("cmd %v, stdout %q, stderr %q", cmd, stdout, stderr)

	if setable {
		Expect(err).NotTo(HaveOccurred(), msg)
	} else {
		Expect(err).To(HaveOccurred(), msg)
	}
}
