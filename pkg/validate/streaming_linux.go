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
	internalapi "github.com/kubernetes-sigs/cri-tools/pkg/cri"
	"github.com/kubernetes-sigs/cri-tools/pkg/framework"
	runtimeapi "k8s.io/cri-api/pkg/apis/runtime/v1"

	. "github.com/onsi/ginkgo"
)

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

		AfterEach(func() {
			By("stop PodSandbox")
			rc.StopPodSandbox(podID)
			By("delete PodSandbox")
			rc.RemovePodSandbox(podID)
		})

		It("runtime should support portforward in host network", func() {
			By("create a PodSandbox with container port port mapping in host network")
			var podConfig *runtimeapi.PodSandboxConfig
			portMappings := []*runtimeapi.PortMapping{
				{
					ContainerPort: webServerHostNetContainerPort,
				},
			}
			podID, podConfig = createPodSandboxWithPortMapping(rc, portMappings, true)

			By("create a web server container")
			containerID := createHostNetWebServerContainer(rc, ic, podID, podConfig, "container-for-host-net-portforward-test")

			By("start the web server container")
			startContainer(rc, containerID)

			req := createDefaultPortForward(rc, podID)

			By("check the output of portforward")
			checkPortForward(rc, req, webServerHostPortForHostNetPortFroward, webServerHostNetContainerPort)
		})

	})
})
