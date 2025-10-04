//go:build integration

/*
 * Copyright 2023 nebuly.com.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package migagent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/nebuly-ai/nos/pkg/api/nos.nebuly.com/v1alpha1"
	"github.com/nebuly-ai/nos/pkg/test/factory"
	mockedmig "github.com/nebuly-ai/nos/pkg/test/mocks/mig"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var cfg *rest.Config
var k8sClient client.Client
var testEnv *envtest.Environment
var (
	ctx               context.Context
	cancel            context.CancelFunc
	actuatorMigClient *mockedmig.Client
	reporterMigClient *mockedmig.Client
	logger            logr.Logger

	actuator            MigActuator
	reporterSharedState *SharedState
)

const (
	actuatorNodeName = "actuator-test"
	reporterNodeName = "reporter-test"

	actuatorNvidiaDevicePluginPodName = "nvidia-device-plugin-actuator"
	reporterNvidiaDevicePluginPodName = "nvidia-device-plugin-reporter"
	nvidiaDevicePluginPodNamespace    = "default"
)

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Controllers Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))
	ctx, cancel = context.WithCancel(context.Background())
	logger = logf.FromContext(ctx)

	By("bootstrapping test environment")
	crdPath := filepath.Join("..", "..", "..", "config", "crd", "bases")
	if info, err := os.Stat(crdPath); err == nil && info.IsDir() {
		testEnv = &envtest.Environment{
			CRDDirectoryPaths:     []string{crdPath},
			ErrorIfCRDPathMissing: true,
		}
	} else {
		if err != nil && !os.IsNotExist(err) {
			Expect(err).NotTo(HaveOccurred())
		}
		testEnv = &envtest.Environment{}
	}

	var err error
	// cfg is defined in this file globally.
	cfg, err = testEnv.Start()
	if err != nil {
		testEnv = nil
		Skip(fmt.Sprintf("skipping envtest: %v", err))
	}
	Expect(cfg).NotTo(BeNil())

	err = v1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	k8sManager, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:             scheme.Scheme,
		MetricsBindAddress: ":8081",
	})
	Expect(err).ToNot(HaveOccurred())

	// Create nodes
	actuatorNode := factory.BuildNode(actuatorNodeName).Get()
	Expect(k8sClient.Create(ctx, &actuatorNode)).To(Succeed())
	reporterNode := factory.BuildNode(reporterNodeName).Get()
	Expect(k8sClient.Create(ctx, &reporterNode)).To(Succeed())

	// Create nvidia-device-plugin pods
	actuatorNvidiaDevicePluginPod := factory.BuildPod(nvidiaDevicePluginPodNamespace, actuatorNvidiaDevicePluginPodName).
		WithLabel("app", "nvidia-device-plugin-daemonset").
		WithNodeName(actuatorNodeName).
		WithContainer(factory.BuildContainer("test", "test").Get()).
		Get()
	reporterNvidiaDevicePluginPod := factory.BuildPod(nvidiaDevicePluginPodNamespace, reporterNvidiaDevicePluginPodName).
		WithLabel("app", "nvidia-device-plugin-daemonset").
		WithNodeName(reporterNodeName).
		WithContainer(factory.BuildContainer("test", "test").Get()).
		Get()
	Expect(k8sClient.Create(ctx, &actuatorNvidiaDevicePluginPod)).To(Succeed())
	Expect(k8sClient.Create(ctx, &reporterNvidiaDevicePluginPod)).To(Succeed())

	// Create Reporter and Actuator
	actuatorMigClient = &mockedmig.Client{}
	reporterMigClient = &mockedmig.Client{}
	actuatorSharedState := NewSharedState()
	reporterSharedState = NewSharedState()

	// Setup Reporter
	reporter := NewReporter(k8sClient, reporterMigClient, reporterSharedState, 3*time.Second)
	err = reporter.SetupWithManager(k8sManager, "MIGReporter", reporterNodeName)
	Expect(err).ToNot(HaveOccurred())

	// Setup Actuator
	actuator = NewActuator(k8sClient, actuatorMigClient, actuatorSharedState, actuatorNodeName)
	err = actuator.SetupWithManager(k8sManager, "MIGActuator")
	Expect(err).ToNot(HaveOccurred())

	go func() {
		defer GinkgoRecover()
		err = k8sManager.Start(ctx)
		Expect(err).ToNot(HaveOccurred(), "failed to run manager")
	}()
})

var _ = AfterSuite(func() {
	cancel()
	By("tearing down the test environment")
	if testEnv != nil {
		err := testEnv.Stop()
		Expect(err).NotTo(HaveOccurred())
	}
})
