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

package kubevirt

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	nadv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	kubevirtapiv1 "kubevirt.io/client-go/api/v1"
	cdiapiv1alpa1 "kubevirt.io/containerized-data-importer/pkg/apis/core/v1alpha1"
)

var (
	kubeConfigEnvName         = "KUBECONFIG"
	kubeConfigDefaultFilename = filepath.Join(os.Getenv("HOME"), ".kube", "config")
)

func LoadKubeConfigContent() ([]byte, error) {
	kubeConfigFilename := os.Getenv(kubeConfigEnvName)
	// Fallback to default kubeconfig file location if no env variable set
	if kubeConfigFilename == "" {
		kubeConfigFilename = kubeConfigDefaultFilename
	}

	return ioutil.ReadFile(kubeConfigFilename)
}

//go:generate mockgen -source=./client.go -destination=./mock/client_generated.go -package=mock

// ClientBuilderFuncType is function type for building infra-cluster clients
type ClientBuilderFuncType func() (Client, error)

// Client is a wrapper object for actual infra-cluster clients: kubernetes and the kubevirt
type Client interface {
	GetNamespace(ctx context.Context, name string) (*corev1.Namespace, error)
	ListNamespace(ctx context.Context) (*corev1.NamespaceList, error)
	GetStorageClass(ctx context.Context, name string) (*storagev1.StorageClass, error)
	GetNetworkAttachmentDefinition(ctx context.Context, name string, namespace string) (*unstructured.Unstructured, error)
	DeleteVirtualMachine(namespace string, name string, wait bool) error
	ListVirtualMachineNames(namespace string, requiredLabels map[string]string) ([]string, error)
	DeleteDataVolume(namespace string, name string, wait bool) error
	ListDataVolumeNames(namespace string, requiredLabels map[string]string) ([]string, error)
	DeleteSecret(namespace string, name string, wait bool) error
	ListSecretNames(namespace string, requiredLabels map[string]string) ([]string, error)
}

type client struct {
	kubernetesClient *kubernetes.Clientset
	dynamicClient    dynamic.Interface
}

// New creates our client wrapper object for the actual kubeVirt and kubernetes clients we use.
func NewClient() (Client, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	// if you want to change the loading rules (which files in which order), you can do so here

	configOverrides := &clientcmd.ConfigOverrides{}
	// if you want to change override values or bind them to flags, there are methods to help you

	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)
	restClientConfig, err := kubeConfig.ClientConfig()
	if err != nil {
		return nil, err
	}

	result := &client{}

	if result.kubernetesClient, err = kubernetes.NewForConfig(restClientConfig); err != nil {
		return nil, err
	}
	if result.dynamicClient, err = dynamic.NewForConfig(restClientConfig); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *client) GetNamespace(ctx context.Context, name string) (*corev1.Namespace, error) {
	return c.kubernetesClient.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
}

func (c *client) ListNamespace(ctx context.Context) (*corev1.NamespaceList, error) {
	return c.kubernetesClient.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
}

func (c *client) GetStorageClass(ctx context.Context, name string) (*storagev1.StorageClass, error) {
	return c.kubernetesClient.StorageV1().StorageClasses().Get(ctx, name, metav1.GetOptions{})
}

func (c *client) GetNetworkAttachmentDefinition(ctx context.Context, name string, namespace string) (*unstructured.Unstructured, error) {
	nadRes := schema.GroupVersionResource{Group: nadv1.SchemeGroupVersion.Group, Version: nadv1.SchemeGroupVersion.Version, Resource: "network-attachment-definitions"}
	return c.getResource(namespace, name, nadRes)
}

// The functions bellow are used for the destroy command
// Use Dynamic cluster for those actions (list and delete)

func (c *client) DeleteVirtualMachine(namespace string, name string, wait bool) error {
	vmRes := schema.GroupVersionResource{Group: kubevirtapiv1.GroupVersion.Group, Version: kubevirtapiv1.GroupVersion.Version, Resource: "virtualmachines"}
	return c.deleteResource(namespace, name, vmRes, wait)
}

func (c *client) ListVirtualMachineNames(namespace string, requiredLabels map[string]string) ([]string, error) {
	vmRes := schema.GroupVersionResource{Group: kubevirtapiv1.GroupVersion.Group, Version: kubevirtapiv1.GroupVersion.Version, Resource: "virtualmachines"}
	return c.listResource(namespace, requiredLabels, vmRes)
}

func (c *client) DeleteDataVolume(namespace string, name string, wait bool) error {
	dvRes := schema.GroupVersionResource{Group: cdiapiv1alpa1.SchemeGroupVersion.Group, Version: cdiapiv1alpa1.SchemeGroupVersion.Version, Resource: "datavolumes"}
	return c.deleteResource(namespace, name, dvRes, wait)
}

func (c *client) ListDataVolumeNames(namespace string, requiredLabels map[string]string) ([]string, error) {
	dvRes := schema.GroupVersionResource{Group: cdiapiv1alpa1.SchemeGroupVersion.Group, Version: cdiapiv1alpa1.SchemeGroupVersion.Version, Resource: "datavolumes"}
	return c.listResource(namespace, requiredLabels, dvRes)
}

func (c *client) DeleteSecret(namespace string, name string, wait bool) error {
	secretRes := schema.GroupVersionResource{Group: corev1.SchemeGroupVersion.Group, Version: corev1.SchemeGroupVersion.Version, Resource: "secrets"}
	return c.deleteResource(namespace, name, secretRes, wait)
}

func (c *client) ListSecretNames(namespace string, requiredLabels map[string]string) ([]string, error) {
	secretRes := schema.GroupVersionResource{Group: corev1.SchemeGroupVersion.Group, Version: corev1.SchemeGroupVersion.Version, Resource: "secrets"}
	return c.listResource(namespace, requiredLabels, secretRes)
}

func (c *client) deleteResource(namespace string, name string, resource schema.GroupVersionResource, wait bool) error {
	if err := c.dynamicClient.Resource(resource).Namespace(namespace).Delete(context.Background(), name, metav1.DeleteOptions{}); err != nil {
		return err
	}
	if !wait {
		return nil
	}
	// If called with wait flag, wait maximum 5 times, each time wait 1 second and check if vm exists
	var getErr error
	counter := 0
	for ; getErr == nil; _, getErr = c.getResource(namespace, name, resource) {
		if counter == 5 {
			return fmt.Errorf("Failed to delete resource %s, checked 5 times and the vm stil exists", name)
		}
		time.Sleep(1 * time.Second)
		counter++
	}
	return nil
}

func (c *client) getResource(namespace string, name string, resource schema.GroupVersionResource) (*unstructured.Unstructured, error) {
	return c.dynamicClient.Resource(resource).Namespace(namespace).Get(context.Background(), name, metav1.GetOptions{})
}

func (c *client) listResource(namespace string, requiredLabels map[string]string, resource schema.GroupVersionResource) ([]string, error) {
	var result []string
	list, err := c.dynamicClient.Resource(resource).Namespace(namespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	for _, d := range list.Items {
		if d.GetNamespace() != namespace {
			continue
		}
		existLabels := d.GetLabels()
		for k, v := range requiredLabels {
			if existVal, ok := existLabels[k]; ok && existVal == v {
				result = append(result, d.GetName())
				break
			}
		}
	}
	return result, nil
}
