package config

import (
	"flag"
	"sync"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

var clientset *kubernetes.Clientset
var once sync.Once

// GetKubeConfig returns a kubeconfig struct
// if inCluster get from the service account
// if not inCluster get from config file
// NOTE: if not inCluster the path of the config should be /pkg/config/config
func GetKubeConfig(inCluster bool) (*rest.Config, error) {
	if !inCluster {
		kubeConfigPath := "/set/your/kube/config/path"
		flag.Parse()
		config, err := clientcmd.BuildConfigFromFlags("", kubeConfigPath)
		return config, err
	} else {
		config, err := rest.InClusterConfig()
		return config, err
	}
}

// 封装了k8s 证书的方式
func GetClientSet() (*kubernetes.Clientset, error) {
	inCluster := true

	//这里这个应该是sync 同步处理，
	once.Do(func() {
		config, err := GetKubeConfig(inCluster)
		if err != nil {
			panic(err)
		}
		clientset, err = kubernetes.NewForConfig(config)
		if err != nil {
			panic(err)
		}
	})

	return clientset, nil

}
