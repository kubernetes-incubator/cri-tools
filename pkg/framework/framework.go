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
	internalapi "github.com/kubernetes-sigs/cri-tools/pkg/cri"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

// Framework will keep a client for you.
type Framework struct {
	// CRI client
	CRIClient *InternalAPIClient
}

// InternalAPIClient is the CRI client.
type InternalAPIClient struct {
	CRIRuntimeClient internalapi.RuntimeService
	CRIImageClient   internalapi.ImageManagerService
}

// NewDefaultCRIFramework makes a new framework and sets up a BeforeEach/AfterEach for
// you (you can write additional before/after each functions).
func NewDefaultCRIFramework() *Framework {
	return NewCRIFramework(nil)
}

// NewCRIFramework creates a new Framework.
func NewCRIFramework(client *InternalAPIClient) *Framework {
	f := &Framework{
		CRIClient: client,
	}

	BeforeEach(f.BeforeEach)
	AfterEach(f.AfterEach)

	return f
}

// BeforeEach gets a client
func (f *Framework) BeforeEach() {
	if f.CRIClient == nil {
		c, err := LoadCRIClient()
		Expect(err).NotTo(HaveOccurred())
		f.CRIClient = c
	}
}

// AfterEach clean resources
func (f *Framework) AfterEach() {
	f.CRIClient = nil
}

// KubeDescribe is a wrapper on Describe.
func KubeDescribe(text string, body func()) bool {
	return Describe("[k8s.io] "+text, body)
}
