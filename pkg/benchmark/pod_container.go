/*
Copyright 2018 The Kubernetes Authors.

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

package benchmark

import (
	internalapi "github.com/kubernetes-sigs/cri-tools/pkg/cri"
	"github.com/kubernetes-sigs/cri-tools/pkg/framework"
	runtimeapi "k8s.io/cri-api/pkg/apis/runtime/v1"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = framework.KubeDescribe("PodSandbox", func() {
	f := framework.NewDefaultCRIFramework()

	var rc internalapi.RuntimeService
	var ic internalapi.ImageManagerService
	var podID string

	BeforeEach(func() {
		rc = f.CRIClient.CRIRuntimeClient
		ic = f.CRIClient.CRIImageClient
	})

	AfterEach(func() {
		By("stop PodSandbox")
		rc.StopPodSandbox(podID)
		By("delete PodSandbox")
		rc.RemovePodSandbox(podID)
	})

	Context("benchmark about start a container from scratch", func() {
		Measure("benchmark about start a container from scratch", func(b Benchmarker) {
			var err error

			podSandboxName := "PodSandbox-for-creating-pod-and-container-performance-test-" + framework.NewUUID()
			uid := framework.DefaultUIDPrefix + framework.NewUUID()
			namespace := framework.DefaultNamespacePrefix + framework.NewUUID()

			config := &runtimeapi.PodSandboxConfig{
				Metadata: framework.BuildPodSandboxMetadata(podSandboxName, uid, namespace, framework.DefaultAttempt),
				Linux:    &runtimeapi.LinuxPodSandboxConfig{},
			}

			operation := b.Time("create PodSandbox and container", func() {
				By("run PodSandbox")
				podID, err = rc.RunPodSandbox(config, framework.TestContext.RuntimeHandler)
				framework.ExpectNoError(err, "failed to create PodSandbox: %v", err)
				By("create container in PodSandbox")
				containerID := framework.CreateDefaultContainer(rc, ic, podID, config, "Pod-Container-for-creating-benchmark-")
				By("start container in PodSandbox")
				err = rc.StartContainer(containerID)
			})

			framework.ExpectNoError(err, "failed to start Container: %v", err)
			Expect(operation.Seconds()).Should(BeNumerically("<", 5), "create PodSandbox shouldn't take too long.")
		}, defaultOperationTimes)
	})
})
