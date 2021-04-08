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

package benchmark

import (
	internalapi "github.com/kubernetes-sigs/cri-tools/pkg/cri"
	"github.com/kubernetes-sigs/cri-tools/pkg/framework"
	runtimeapi "k8s.io/cri-api/pkg/apis/runtime/v1"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = framework.KubeDescribe("Container", func() {
	f := framework.NewDefaultCRIFramework()

	var rc internalapi.RuntimeService
	var ic internalapi.ImageManagerService

	BeforeEach(func() {
		rc = f.CRIClient.CRIRuntimeClient
		ic = f.CRIClient.CRIImageClient
	})

	Context("benchmark about operations on Container", func() {
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

		Measure("benchmark about basic operations on Container", func(b Benchmarker) {
			var containerID string
			var err error

			operation := b.Time("create Container", func() {
				By("benchmark about creating Container")
				containerID = framework.CreateDefaultContainer(rc, ic, podID, podConfig, "Container-for-creating-benchmark-")
			})
			Expect(operation.Seconds()).Should(BeNumerically("<", 2), "create Container shouldn't take too long.")

			operation = b.Time("start Container", func() {
				By("benchmark about starting Container")
				err = rc.StartContainer(containerID)
			})

			framework.ExpectNoError(err, "failed to start Container: %v", err)
			Expect(operation.Seconds()).Should(BeNumerically("<", 2), "start Container shouldn't take too long.")

			operation = b.Time("Container status", func() {
				By("benchmark about getting Container status")
				_, err = rc.ContainerStatus(containerID)
			})

			framework.ExpectNoError(err, "failed to get Container status: %v", err)
			Expect(operation.Seconds()).Should(BeNumerically("<", 2), "get container status shouldn't take too long.")

			operation = b.Time("stop Container", func() {
				By("benchmark about stoping Container")
				err = rc.StopContainer(containerID, framework.DefaultStopContainerTimeout)
			})

			framework.ExpectNoError(err, "failed to stop Container: %v", err)
			Expect(operation.Seconds()).Should(BeNumerically("<", 2), "stop Container shouldn't take too long.")

			operation = b.Time("remove Container", func() {
				By("benchmark about removing Container")
				err = rc.RemoveContainer(containerID)
			})

			framework.ExpectNoError(err, "failed to remove Container: %v", err)
			Expect(operation.Seconds()).Should(BeNumerically("<", 2), "remove Container shouldn't take too long.")

		}, defaultOperationTimes)

		Measure("benchmark about listing Container", func(b Benchmarker) {
			containerList := make([]string, 0, framework.TestContext.Number)
			var err error

			for i := 0; i < framework.TestContext.Number; i++ {
				containerID := framework.CreateDefaultContainer(rc, ic, podID, podConfig, "Container-for-listing-benchmark-")
				containerList = append(containerList, containerID)
			}

			operation := b.Time("list Container", func() {
				_, err = rc.ListContainers(nil)
			})

			framework.ExpectNoError(err, "failed to list Container: %v", err)
			Expect(operation.Seconds()).Should(BeNumerically("<", 2), "list Container shouldn't take too long.")

			for _, containerID := range containerList {
				rc.StopContainer(containerID, framework.DefaultStopContainerTimeout)
				rc.RemoveContainer(containerID)
			}
		}, defaultOperationTimes)
	})
})
