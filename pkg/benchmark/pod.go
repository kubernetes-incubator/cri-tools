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

const (
	defaultOperationTimes int = 20
)

var _ = framework.KubeDescribe("PodSandbox", func() {
	f := framework.NewDefaultCRIFramework()

	var c internalapi.RuntimeService

	BeforeEach(func() {
		c = f.CRIClient.CRIRuntimeClient
	})

	Context("benchmark about operations on PodSandbox", func() {
		Measure("benchmark about lifecycle of PodSandbox", func(b Benchmarker) {
			var podID string
			var err error

			podSandboxName := "PodSandbox-for-creating-performance-test-" + framework.NewUUID()
			uid := framework.DefaultUIDPrefix + framework.NewUUID()
			namespace := framework.DefaultNamespacePrefix + framework.NewUUID()

			config := &runtimeapi.PodSandboxConfig{
				Metadata: framework.BuildPodSandboxMetadata(podSandboxName, uid, namespace, framework.DefaultAttempt),
				Linux:    &runtimeapi.LinuxPodSandboxConfig{},
				Labels:   framework.DefaultPodLabels,
			}

			// TODO: add support of runtime
			operation := b.Time("create PodSandbox", func() {
				podID, err = c.RunPodSandbox(config, framework.TestContext.RuntimeHandler)
			})

			framework.ExpectNoError(err, "failed to create PodSandbox: %v", err)
			Expect(operation.Seconds()).Should(BeNumerically("<", 5), "create PodSandbox shouldn't take too long.")

			operation = b.Time("PodSandbox status", func() {
				_, err = c.PodSandboxStatus(podID)
			})

			framework.ExpectNoError(err, "failed to get PodSandbox status: %v", err)
			Expect(operation.Seconds()).Should(BeNumerically("<", 5), "get PodSandbox status shouldn't take too long.")

			operation = b.Time("stop PodSandbox", func() {
				err = c.StopPodSandbox(podID)
			})

			framework.ExpectNoError(err, "failed to stop PodSandbox: %v", err)
			Expect(operation.Seconds()).Should(BeNumerically("<", 5), "stop PodSandbox shouldn't take too long.")

			operation = b.Time("remove PodSandbox", func() {
				c.RemovePodSandbox(podID)
			})

			framework.ExpectNoError(err, "failed to remove PodSandbox: %v", err)
			Expect(operation.Seconds()).Should(BeNumerically("<", 5), "remove PodSandbox shouldn't take too long.")
		}, defaultOperationTimes)

		Measure("benchmark about listing PodSandbox", func(b Benchmarker) {
			podList := make([]string, 0, framework.TestContext.Number)
			var err error

			for i := 0; i < framework.TestContext.Number; i++ {
				podID := framework.RunDefaultPodSandbox(c, "PodSandbox-for-list-benchmark-")
				podList = append(podList, podID)
			}

			operation := b.Time("list PodSandbox", func() {
				_, err = c.ListPodSandbox(nil)
			})

			framework.ExpectNoError(err, "failed to list PodSandbox: %v", err)
			Expect(operation.Seconds()).Should(BeNumerically("<", 5), "list PodSandbox shouldn't take too long.")

			for _, podID := range podList {
				c.StopPodSandbox(podID)
				c.RemovePodSandbox(podID)
			}
		}, defaultOperationTimes)
	})
})
