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

package framework

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/pborman/uuid"
	internalapi "k8s.io/kubernetes/pkg/kubelet/apis/cri"
	runtimeapi "k8s.io/kubernetes/pkg/kubelet/apis/cri/v1alpha1/runtime"
	"k8s.io/kubernetes/pkg/kubelet/remote"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var (
	//lock for uuid
	uuidLock sync.Mutex

	// lastUUID record last generated uuid from NewUUID()
	lastUUID uuid.UUID
)

const (
	// DefaultUIDPrefix is a default UID prefix of PodSandbox
	DefaultUIDPrefix string = "cri-test-uid"

	// DefaultNamespacePrefix is a default namespace prefix of PodSandbox
	DefaultNamespacePrefix string = "cri-test-namespace"

	// DefaultAttempt is a default attempt prefix of PodSandbox or container
	DefaultAttempt uint32 = 2

	// DefaultContainerImage is the default image for container using
	DefaultContainerImage string = "busybox:1.26"

	// DefaultStopContainerTimeout is the default timeout for stopping container
	DefaultStopContainerTimeout int64 = 60
)

// LoadCRIClient creates a InternalAPIClient.
func LoadCRIClient() (*InternalAPIClient, error) {
	rService, err := remote.NewRemoteRuntimeService(TestContext.RuntimeServiceAddr, TestContext.RuntimeServiceTimeout)
	if err != nil {
		return nil, err
	}

	iService, err := remote.NewRemoteImageService(TestContext.ImageServiceAddr, TestContext.ImageServiceTimeout)
	if err != nil {
		return nil, err
	}

	return &InternalAPIClient{
		CRIRuntimeClient: rService,
		CRIImageClient:   iService,
	}, nil
}

func nowStamp() string {
	return time.Now().Format(time.StampMilli)
}

func log(level string, format string, args ...interface{}) {
	fmt.Fprintf(GinkgoWriter, nowStamp()+": "+level+": "+format+"\n", args...)
}

// Logf prints a info message.
func Logf(format string, args ...interface{}) {
	log("INFO", format, args...)
}

// Failf prints an error message.
func Failf(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	log("INFO", msg)
	Fail(nowStamp()+": "+msg, 1)
}

// ExpectNoError reports error if err is not nil.
func ExpectNoError(err error, explain ...interface{}) {
	if err != nil {
		Logf("Unexpected error occurred: %v", err)
	}
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), explain...)
}

// NewUUID creates a new UUID string.
func NewUUID() string {
	uuidLock.Lock()
	defer uuidLock.Unlock()
	result := uuid.NewUUID()
	// The UUID package is naive and can generate identical UUIDs if the
	// time interval is quick enough.
	// The UUID uses 100 ns increments so it's short enough to actively
	// wait for a new value.
	for uuid.Equal(lastUUID, result) == true {
		result = uuid.NewUUID()
	}
	lastUUID = result
	return result.String()
}

// RunDefaultPodSandbox runs a PodSandbox with default options.
func RunDefaultPodSandbox(c internalapi.RuntimeService, prefix string) string {
	podSandboxName := prefix + NewUUID()
	uid := DefaultUIDPrefix + NewUUID()
	namespace := DefaultNamespacePrefix + NewUUID()

	config := &runtimeapi.PodSandboxConfig{
		Metadata: BuildPodSandboxMetadata(podSandboxName, uid, namespace, DefaultAttempt),
		Linux: &runtimeapi.LinuxPodSandboxConfig{
			SecurityContext: &runtimeapi.LinuxSandboxSecurityContext{},
		},
	}
	return RunPodSandbox(c, config)
}

// BuildPodSandboxMetadata builds PodSandboxMetadata.
func BuildPodSandboxMetadata(podSandboxName, uid, namespace string, attempt uint32) *runtimeapi.PodSandboxMetadata {
	return &runtimeapi.PodSandboxMetadata{
		Name:      podSandboxName,
		Uid:       uid,
		Namespace: namespace,
		Attempt:   attempt,
	}
}

// RunPodSandbox runs a PodSandbox.
func RunPodSandbox(c internalapi.RuntimeService, config *runtimeapi.PodSandboxConfig) string {
	podID, err := c.RunPodSandbox(config)
	ExpectNoError(err, "failed to create PodSandbox: %v", err)
	return podID
}

// CreatePodSandboxForContainer creates a PodSandbox for creating containers.
func CreatePodSandboxForContainer(c internalapi.RuntimeService) (string, *runtimeapi.PodSandboxConfig) {
	podSandboxName := "create-PodSandbox-for-container-" + NewUUID()
	uid := DefaultUIDPrefix + NewUUID()
	namespace := DefaultNamespacePrefix + NewUUID()
	config := &runtimeapi.PodSandboxConfig{
		Metadata: BuildPodSandboxMetadata(podSandboxName, uid, namespace, DefaultAttempt),
		Linux: &runtimeapi.LinuxPodSandboxConfig{
			SecurityContext: &runtimeapi.LinuxSandboxSecurityContext{},
		},
	}

	podID := RunPodSandbox(c, config)
	return podID, config
}

// BuildContainerMetadata builds containerMetadata.
func BuildContainerMetadata(containerName string, attempt uint32) *runtimeapi.ContainerMetadata {
	return &runtimeapi.ContainerMetadata{
		Name:    containerName,
		Attempt: attempt,
	}
}

// CreateDefaultContainer creates a  default container with default options.
func CreateDefaultContainer(rc internalapi.RuntimeService, ic internalapi.ImageManagerService, podID string, podConfig *runtimeapi.PodSandboxConfig, prefix string) string {
	containerName := prefix + NewUUID()
	containerConfig := &runtimeapi.ContainerConfig{
		Metadata: BuildContainerMetadata(containerName, DefaultAttempt),
		Image:    &runtimeapi.ImageSpec{Image: DefaultContainerImage},
		Command:  []string{"top"},
		Linux:    &runtimeapi.LinuxContainerConfig{},
	}

	return CreateContainer(rc, ic, containerConfig, podID, podConfig)
}

// CreateContainer creates a container with the prefix of containerName.
func CreateContainer(rc internalapi.RuntimeService, ic internalapi.ImageManagerService, config *runtimeapi.ContainerConfig, podID string, podConfig *runtimeapi.PodSandboxConfig) string {
	// Pull the image if it does not exist.
	imageName := config.Image.Image
	if !strings.Contains(imageName, ":") {
		imageName = imageName + ":latest"
		Logf("Use latest as default image tag.")
	}

	status := ImageStatus(ic, imageName)
	if status == nil {
		PullPublicImage(ic, imageName)
	}

	By("Create container.")
	containerID, err := rc.CreateContainer(podID, config, podConfig)
	ExpectNoError(err, "failed to create container: %v", err)
	Logf("Created container %q\n", containerID)
	return containerID
}

// ImageStatus gets the status of the image named imageName.
func ImageStatus(c internalapi.ImageManagerService, imageName string) *runtimeapi.Image {
	By("Get image status for image: " + imageName)
	imageSpec := &runtimeapi.ImageSpec{
		Image: imageName,
	}
	status, err := c.ImageStatus(imageSpec)
	ExpectNoError(err, "failed to get image status: %v", err)
	return status
}

// ListImage list the image filtered by the image filter.
func ListImage(c internalapi.ImageManagerService, filter *runtimeapi.ImageFilter) []*runtimeapi.Image {
	images, err := c.ListImages(filter)
	ExpectNoError(err, "Failed to get image list: %v", err)
	return images
}

// PullPublicImage pulls the public image named imageName.
func PullPublicImage(c internalapi.ImageManagerService, imageName string) {
	if !strings.Contains(imageName, ":") {
		imageName = imageName + ":latest"
		Logf("Use latest as default image tag.")
	}

	By("Pull image : " + imageName)
	imageSpec := &runtimeapi.ImageSpec{
		Image: imageName,
	}
	_, err := c.PullImage(imageSpec, nil)
	ExpectNoError(err, "failed to pull image: %v", err)
}
