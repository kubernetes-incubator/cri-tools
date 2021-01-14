/*
Copyright 2019 The Kubernetes Authors.

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
	"runtime"

	"github.com/kubernetes-sigs/cri-tools/pkg/framework"
	runtimeapi "k8s.io/cri-api/pkg/apis/runtime/v1"
)

// Container test constants

var (
	echoHelloCmd      []string
	sleepCmd          []string
	checkSleepCmd     []string
	shellCmd          []string
	pauseCmd          []string
	logDefaultCmd     []string
	loopLogDefaultCmd []string
	echoHelloOutput   string
	checkPathCmd      func(string) []string

	// Linux defaults
	echoHelloLinuxCmd      = []string{"echo", "-n", "hello"}
	sleepLinuxCmd          = []string{"sleep", "4321"}
	checkSleepLinuxCmd     = []string{"sh", "-c", "pgrep sleep || true"}
	shellLinuxCmd          = []string{"/bin/sh"}
	pauseLinuxCmd          = []string{"sh", "-c", "top"}
	logDefaultLinuxCmd     = []string{"echo", defaultLog}
	loopLogDefaultLinuxCmd = []string{"sh", "-c", "while true; do echo " + defaultLog + "; sleep 1; done"}
	echoHelloLinuxOutput   = "hello"
	checkPathLinuxCmd      = func(path string) []string { return []string{"ls", "-A", path} }

	// Windows defaults
	echoHelloWindowsCmd      = []string{"powershell", "-c", "echo hello"}
	sleepWindowsCmd          = []string{"powershell", "-c", "sleep", "4321"}
	checkSleepWindowsCmd     = []string{"powershell", "-c", "tasklist | findstr sleep; exit 0"}
	shellWindowsCmd          = []string{"cmd", "/Q"}
	pauseWindowsCmd          = []string{"powershell", "-c", "ping -t localhost"}
	logDefaultWindowsCmd     = []string{"powershell", "-c", "echo '" + defaultLog + "'"}
	loopLogDefaultWindowsCmd = []string{"powershell", "-c", "while($true) { echo '" + defaultLog + "'; sleep 1; }"}
	echoHelloWindowsOutput   = "hello\r\n"
	checkPathWindowsCmd      = func(path string) []string { return []string{"powershell", "-c", "ls", path} }
)

var _ = framework.AddBeforeSuiteCallback(func() {
	if runtime.GOOS != "windows" || framework.TestContext.IsLcow {
		echoHelloCmd = echoHelloLinuxCmd
		sleepCmd = sleepLinuxCmd
		checkSleepCmd = checkSleepLinuxCmd
		shellCmd = shellLinuxCmd
		pauseCmd = pauseLinuxCmd
		logDefaultCmd = logDefaultLinuxCmd
		loopLogDefaultCmd = loopLogDefaultLinuxCmd
		echoHelloOutput = echoHelloLinuxOutput
		checkPathCmd = checkPathLinuxCmd
	} else {
		echoHelloCmd = echoHelloWindowsCmd
		sleepCmd = sleepWindowsCmd
		checkSleepCmd = checkSleepWindowsCmd
		shellCmd = shellWindowsCmd
		pauseCmd = pauseWindowsCmd
		logDefaultCmd = logDefaultWindowsCmd
		loopLogDefaultCmd = loopLogDefaultWindowsCmd
		echoHelloOutput = echoHelloWindowsOutput
		checkPathCmd = checkPathWindowsCmd
	}
})

// Image test constants

const (
	testImageUserUID           = "gcr.io/cri-tools/test-image-user-uid"
	imageUserUID               = int64(1002)
	testImageUserUsername      = "gcr.io/cri-tools/test-image-user-username"
	imageUserUsername          = "www-data"
	testImageUserUIDGroup      = "gcr.io/cri-tools/test-image-user-uid-group"
	imageUserUIDGroup          = int64(1003)
	testImageUserUsernameGroup = "gcr.io/cri-tools/test-image-user-username-group"
	imageUserUsernameGroup     = "www-data"

	// Linux defaults
	testLinuxImageWithoutTag        = "gcr.io/cri-tools/test-image-latest"
	testLinuxImageWithTag           = "gcr.io/cri-tools/test-image-tag:test"
	testLinuxImageWithDigest        = "gcr.io/cri-tools/test-image-digest@sha256:9179135b4b4cc5a8721e09379244807553c318d92fa3111a65133241551ca343"
	testLinuxImageWithAllReferences = "gcr.io/cri-tools/test-image-tag:all"

	// Windows defaults
	testWindowsImageWithoutTag        = "gcr.io/cri-tools/win-test-image-latest"
	testWindowsImageWithTag           = "gcr.io/cri-tools/win-test-image-tag:test"
	testWindowsImageWithDigest        = "gcr.io/cri-tools/win-test-image-digest@sha256:ed127b3a098d6ada53fff1b33ab3ea421dc7ebb06e0c2abded9d3e84bb6842b0"
	testWindowsImageWithAllReferences = "gcr.io/cri-tools/win-test-image-tag:all"
)

var (
	// image reference without tag
	testImageWithoutTag string

	// name-tagged reference for test image
	testImageWithTag string

	// digested reference for test image
	testImageWithDigest string

	// image used to test all kinds of references.
	testImageWithAllReferences string

	// image list where different tags refer to different images
	testDifferentTagDifferentImageList []string

	// image list where different tags refer to the same image
	testDifferentTagSameImageList []string

	// pod sandbox to use when pulling images
	testImagePodSandbox *runtimeapi.PodSandboxConfig

	// Linux defaults
	testLinuxDifferentTagDifferentImageList = []string{
		"gcr.io/cri-tools/test-image-1:latest",
		"gcr.io/cri-tools/test-image-2:latest",
		"gcr.io/cri-tools/test-image-3:latest",
	}
	testLinuxDifferentTagSameImageList = []string{
		"gcr.io/cri-tools/test-image-tags:1",
		"gcr.io/cri-tools/test-image-tags:2",
		"gcr.io/cri-tools/test-image-tags:3",
	}

	// Windows defaults
	testWindowsDifferentTagDifferentImageList = []string{
		"gcr.io/cri-tools/win-test-image-1:latest",
		"gcr.io/cri-tools/win-test-image-2:latest",
		"gcr.io/cri-tools/win-test-image-3:latest",
	}
	testWindowsDifferentTagSameImageList = []string{
		"gcr.io/cri-tools/win-test-image-tags:1",
		"gcr.io/cri-tools/win-test-image-tags:2",
		"gcr.io/cri-tools/win-test-image-tags:3",
	}
)

var _ = framework.AddBeforeSuiteCallback(func() {
	if runtime.GOOS != "windows" || framework.TestContext.IsLcow {
		testImageWithoutTag = testLinuxImageWithoutTag
		testImageWithTag = testLinuxImageWithTag
		testImageWithDigest = testLinuxImageWithDigest
		testImageWithAllReferences = testLinuxImageWithAllReferences
		testDifferentTagDifferentImageList = testLinuxDifferentTagDifferentImageList
		testDifferentTagSameImageList = testLinuxDifferentTagSameImageList
	} else {
		testImageWithoutTag = testWindowsImageWithoutTag
		testImageWithTag = testWindowsImageWithTag
		testImageWithDigest = testWindowsImageWithDigest
		testImageWithAllReferences = testWindowsImageWithAllReferences
		testDifferentTagDifferentImageList = testWindowsDifferentTagDifferentImageList
		testDifferentTagSameImageList = testWindowsDifferentTagSameImageList
	}
	testImagePodSandbox = &runtimeapi.PodSandboxConfig{
		Labels: framework.DefaultPodLabels,
	}
})

// Networking test constants

const (
	resolvConfigPath              = "/etc/resolv.conf"
	defaultDNSServer       string = "10.10.10.10"
	defaultDNSSearch       string = "google.com"
	defaultDNSOption       string = "ndots:8"
	webServerContainerPort int32  = 80
	// The following host ports must not be in-use when running the test.
	webServerHostPortForPortMapping        int32 = 12000
	webServerHostPortForPortForward        int32 = 12001
	webServerHostPortForHostNetPortFroward int32 = 12002
	// The port used in hostNetNginxImage (See images/hostnet-nginx/)
	webServerHostNetContainerPort int32 = 12003

	// Linux defaults
	webServerLinuxImage        = "nginx"
	hostNetWebServerLinuxImage = "gcr.io/cri-tools/hostnet-nginx-" + runtime.GOARCH

	// Windows defaults
	webServerWindowsImage        = "mcr.microsoft.com/windows/servercore/iis:windowsservercore-ltsc2019"
	hostNetWebServerWindowsImage = "mcr.microsoft.com/windows/servercore/iis:windowsservercore-ltsc2019"
)

var (
	webServerImage        string
	hostNetWebServerImage string
	getDNSConfigCmd       []string
	getDNSConfigContent   []string
	getHostnameCmd        []string

	// Linux defaults
	getDNSConfigLinuxCmd     = []string{"cat", resolvConfigPath}
	getDNSConfigLinuxContent = []string{
		"nameserver " + defaultDNSServer,
		"search " + defaultDNSSearch,
		"options " + defaultDNSOption,
	}
	getHostnameLinuxCmd = []string{"hostname"}

	// Windows defaults
	// Windows doesn't support ndots options.
	// https://kubernetes.io/docs/setup/production-environment/windows/intro-windows-in-kubernetes/#dns-limitations
	getDNSConfigWindowsCmd     = []string{"powershell", "/c", "ipconfig /all"}
	getDNSConfigWindowsContent = []string{
		"DNS Servers . . . . . . . . . . . : " + defaultDNSServer,
		"DNS Suffix Search List. . . . . . : " + defaultDNSSearch,
	}
	getHostnameWindowsCmd = []string{"powershell", "/c", "$env:computername"}
)

var _ = framework.AddBeforeSuiteCallback(func() {
	if runtime.GOOS != "windows" || framework.TestContext.IsLcow {
		webServerImage = webServerLinuxImage
		hostNetWebServerImage = hostNetWebServerLinuxImage
		getDNSConfigCmd = getDNSConfigLinuxCmd
		getDNSConfigContent = getDNSConfigLinuxContent
		getHostnameCmd = getHostnameLinuxCmd
	} else {
		webServerImage = webServerWindowsImage
		hostNetWebServerImage = hostNetWebServerWindowsImage
		getDNSConfigCmd = getDNSConfigWindowsCmd
		getDNSConfigContent = getDNSConfigWindowsContent
		getHostnameCmd = getHostnameWindowsCmd
	}
})

// Streaming test constants

const (
	defaultStreamServerAddress string = "127.0.0.1:10250"
	defaultStreamServerScheme  string = "http"

	// Linux defaults
	attachEchoHelloLinuxOutput = "hello"

	// Windows defaults
	attachEchoHelloWindowsOutput = "hello\r\n\r\nC:\\>"
)

var (
	attachEchoHelloOutput string
)

var _ = framework.AddBeforeSuiteCallback(func() {
	if runtime.GOOS != "windows" || framework.TestContext.IsLcow {
		attachEchoHelloOutput = attachEchoHelloLinuxOutput
	} else {
		attachEchoHelloOutput = attachEchoHelloWindowsOutput
	}
})
