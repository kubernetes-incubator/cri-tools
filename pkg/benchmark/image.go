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
	"runtime"

	internalapi "github.com/kubernetes-sigs/cri-tools/pkg/cri"
	"github.com/kubernetes-sigs/cri-tools/pkg/framework"
	runtimeapi "k8s.io/cri-api/pkg/apis/runtime/v1"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = framework.KubeDescribe("Container", func() {
	f := framework.NewDefaultCRIFramework()

	var ic internalapi.ImageManagerService

	BeforeEach(func() {
		ic = f.CRIClient.CRIImageClient
	})

	Context("benchmark about operations on Image", func() {
		var err error

		var testImageList []string
		if runtime.GOARCH == "amd64" {
			testImageList = []string{
				"busybox:1.26.2-glibc",
				"busybox:1-uclibc",
				"busybox:1",
				"busybox:1-glibc",
				"busybox:1-musl",
			}
		} else {
			testImageList = []string{
				"busybox:1",
				"busybox:1-glibc",
				"busybox:1-musl",
			}
		}

		AfterEach(func() {
			for _, imageName := range testImageList {
				imageSpec := &runtimeapi.ImageSpec{
					Image: imageName,
				}
				ic.RemoveImage(imageSpec)
			}
		})

		Measure("benchmark about basic operations on Image", func(b Benchmarker) {
			imageSpec := &runtimeapi.ImageSpec{
				Image: testImageList[0],
			}

			operation := b.Time("pull Image", func() {
				framework.PullPublicImage(ic, testImageList[0], nil)
			})
			Expect(operation.Minutes()).Should(BeNumerically("<", 1), "pull Image shouldn't take too long.")

			operation = b.Time("Image status", func() {
				_, err = ic.ImageStatus(imageSpec)
			})

			framework.ExpectNoError(err, "failed to get image status: %v", err)
			Expect(operation.Seconds()).Should(BeNumerically("<", 2), "get image status shouldn't take too long.")

			operation = b.Time("remove Image", func() {
				err = ic.RemoveImage(imageSpec)
			})

			framework.ExpectNoError(err, "failed to remove image: %v", err)
			Expect(operation.Seconds()).Should(BeNumerically("<", 2), "remove Image shouldn't take too long.")

		}, defaultOperationTimes)

		Measure("benchmark about listing Image", func(b Benchmarker) {
			for _, imageName := range testImageList {
				framework.PullPublicImage(ic, imageName, nil)
			}

			operation := b.Time("list Container", func() {
				_, err = ic.ListImages(nil)
			})

			framework.ExpectNoError(err, "failed to list Image: %v", err)
			Expect(operation.Seconds()).Should(BeNumerically("<", 2), "list Image shouldn't take too long.")
		}, defaultOperationTimes)
	})
})
