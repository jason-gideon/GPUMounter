package main

import (
	gpu_mount "GPUMounter/pkg/api/gpu-mount"
	"GPUMounter/pkg/config"
	. "GPUMounter/pkg/util/log"
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/julienschmidt/httprouter"
	"google.golang.org/grpc"
	corev1 "k8s.io/api/core/v1"
	k8s_errors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func Index(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	Logger.Info("access home page")
	fmt.Fprint(w, "This is gpu mounter api!\n")
}

// gpuNum是啥意思？几个？如果是要mount具体某个GPU呢？
func AddGPU(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	Logger.Info("access add gpu service")

	//获取参数
	podName := ps.ByName("pod")
	namespace := ps.ByName("namespace")
	gpuNum_str := ps.ByName("gpuNum")
	isEntireMountStr := ps.ByName("isEntireMount")
	Logger.Info("Pod: ", podName, " Namespace: ", namespace, " GPU Num: ", gpuNum_str, " Is entire mount: ", isEntireMountStr)

	//GPU num是什么？
	gpuNum, err := strconv.ParseInt(gpuNum_str, 10, 32)
	if err != nil {
		Logger.Error("Invalid param gpuNum: ", gpuNum_str)
		http.Error(w, "Invalid param gpuNum: "+gpuNum_str, 400)
		return
	}

	isEntireMount, err := strconv.ParseBool(isEntireMountStr)
	if err != nil {
		Logger.Errorf("Invalid param isEntireMount: %s", isEntireMountStr)
		http.Error(w, "Invalid param isEntireMount: "+isEntireMountStr+"(should be true or false)", 400)
		return
	}

	//获取K8s 交互的证书
	clientset, err := config.GetClientSet()
	if err != nil {
		Logger.Error("Connect to k8s failed")
		Logger.Error(err.Error())
		http.Error(w, err.Error(), 500)
		return
	}

	//获取ns下所有pod
	pod, err := clientset.CoreV1().Pods(namespace).Get(context.TODO(), podName, metav1.GetOptions{})
	if err != nil {
		if k8s_errors.IsNotFound(err) {
			Logger.Error("No pod: " + podName + " in namespace: " + namespace)
			Logger.Error(err)
			http.Error(w, "No pod: "+podName+" in namespace: "+namespace, 404)
			return
		} else {
			Logger.Error(err)
			http.Error(w, err.Error(), 500)
			return
		}
	}
	nodeName := pod.Spec.NodeName
	Logger.Info("Found Pod: ", podName, " in Namespace: ", namespace, " on Node: ", nodeName)

	workerMap, err := findAllWorker()
	if err != nil {
		Logger.Error("Failed to found gpu mounter workers")
		Logger.Error(err)
		http.Error(w, err.Error(), 500)
		return
	}
	worker, ok := workerMap[nodeName]
	if !ok {
		Logger.Error("Failed found gpu mounter on Node: ", nodeName)
		http.Error(w, "Service Internal Error", 500)
		return
	}
	workerIP := worker.Status.PodIP
	conn, err := grpc.Dial(workerIP+":1200", grpc.WithInsecure())
	if err != nil {
		Logger.Error("Failed to connect to gpu mounter worker")
		Logger.Error(err)
		http.Error(w, "Service Internal Error", 500)
		return
	}
	defer conn.Close()

	//这里给Pod添加的GPU，GRPC告知worker去添加GPU
	c := gpu_mount.NewAddGPUServiceClient(conn)
	resp, err := c.AddGPU(context.TODO(), &gpu_mount.AddGPURequest{
		PodName:       podName,
		Namespace:     namespace,
		GpuNum:        int32(gpuNum),
		IsEntireMount: isEntireMount,
	})
	if err != nil {
		Logger.Error("Failed to call add gpu service")
		Logger.Error(err)
		http.Error(w, "Service Internal Error", 500)
		return
	}

	//打印结果
	switch resp.AddGpuResult {
	case gpu_mount.AddGPUResponse_Success:
		Logger.Info("Successfully add gpu for Pod: ", podName)
		fmt.Fprintf(w, "Add GPU Success\n")
		return
	case gpu_mount.AddGPUResponse_InsufficientGPU:
		Logger.Error("Insufficient GPU on Node: " + nodeName)
		http.Error(w, "Insufficient GPU on Node: "+nodeName, 500)
		return
	case gpu_mount.AddGPUResponse_PodNotFound:
		Logger.Error("No Pod" + podName + " on Node: " + nodeName)
		http.Error(w, "No Pod"+podName+" on Node: "+nodeName, 400)
		return
	}
}

func RemoveGPU(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	Logger.Info("access remove gpu service")
	err := r.ParseForm()
	if err != nil {
		Logger.Error(err)
		http.Error(w, "Service Internal Error", 500)
		return
	}

	uuids, ok := r.Form["uuids"]
	if !ok {
		Logger.Error("no uuids in request")
		http.Error(w, "Invalid parameter", 400)
		return
	}
	Logger.Info(strings.Join(uuids, ","))
	Logger.Info(uuids[0])

	podName := ps.ByName("pod")
	namespace := ps.ByName("namespace")
	force_str := ps.ByName("force")
	force, err := strconv.ParseBool(force_str)
	if err != nil {
		Logger.Errorf("Invalid param force: " + force_str)
		http.Error(w, "Invalid parameter force: "+force_str+"(should be true or false)", 400)
		return
	}
	Logger.Info("Pod: ", podName, " Namespace: ", namespace, " UUIDs: ", strings.Join(uuids, ", "), " force: ", force)

	clientset, err := config.GetClientSet()
	if err != nil {
		Logger.Error("Connect to k8s failed")
		Logger.Error(err.Error())
		http.Error(w, err.Error(), 500)
		return
	}
	pod, err := clientset.CoreV1().Pods(namespace).Get(context.TODO(), podName, metav1.GetOptions{})
	if err != nil {
		if k8s_errors.IsNotFound(err) {
			Logger.Error("No pod: " + podName + " in namespace: " + namespace)
			Logger.Error(err)
			http.Error(w, "No pod: "+podName+" in namespace: "+namespace, 404)
			return
		} else {
			Logger.Error(err)
			http.Error(w, err.Error(), 500)
			return
		}
	}
	nodeName := pod.Spec.NodeName
	Logger.Info("Found Pod: ", podName, " in Namespace: ", namespace, " on Node: ", nodeName)

	workerMap, err := findAllWorker()
	if err != nil {
		Logger.Error("Failed to found gpu mounter workers")
		Logger.Error(err)
		http.Error(w, err.Error(), 500)
		return
	}
	worker, ok := workerMap[nodeName]
	if !ok {
		Logger.Error("Failed found gpu mounter on Node: ", nodeName)
		http.Error(w, "Service Internal Error", 500)
		return
	}
	workerIP := worker.Status.PodIP
	conn, err := grpc.Dial(workerIP+":1200", grpc.WithInsecure())
	if err != nil {
		Logger.Error("Failed to connect to gpu mounter worker")
		Logger.Error(err)
		http.Error(w, "Service Internal Error", 500)
		return
	}
	defer conn.Close()
	c := gpu_mount.NewRemoveGPUServiceClient(conn)
	resp, err := c.RemoveGPU(context.TODO(), &gpu_mount.RemoveGPURequest{
		PodName:   podName,
		Namespace: namespace,
		Uuids:     uuids,
		Force:     force,
	})
	if err != nil {
		Logger.Error("Failed to call remove gpu service")
		Logger.Error(err)
		http.Error(w, "Service Internal Error", 500)
		return
	}
	switch resp.RemoveGpuResult {
	case gpu_mount.RemoveGPUResponse_PodNotFound:
		Logger.Error("No such Pod: ", pod.Name, " in Namespace: ", pod.Namespace)
		Logger.Error("No Pod" + podName + " on Node: " + nodeName)
		http.Error(w, "No Pod"+podName+" on Node: "+nodeName, 400)
		return
	case gpu_mount.RemoveGPUResponse_GPUBusy:
		Logger.Error("Pod: ", pod.Name, " has running processes on GPU: ", strings.Join(uuids, ", "))
		http.Error(w, "Pod: "+pod.Name+" has running processes on GPU: "+strings.Join(uuids, ", "), 400)
		return
	case gpu_mount.RemoveGPUResponse_GPUNotFound:
		Logger.Error("Invalid UUIDs: ", strings.Join(uuids, ", "))
		http.Error(w, "Invalid UUIDs: "+strings.Join(uuids, ", "), 400)
		return
	case gpu_mount.RemoveGPUResponse_Success:
		Logger.Info("Successfully remove ", len(uuids), " GPUs: ", strings.Join(uuids, ", "))
		fmt.Fprintf(w, "Remove GPU Success\n")
		return
	}
}

// master节点代码，从readme查看，是个http service，等待两个cmd，添加和删除GPU
func main() {
	//初始化log，这个log怎么man函数退出的时候sync。。？不过也对，退出的时候进行同步操作
	InitLogger("/var/log/GPUMounter/", "GPUMounter-master.log")
	defer Logger.Sync()

	//创建http的service
	router := httprouter.New()

	//指定router的接口
	router.GET("/", Index)
	//gpuNum是啥意思？几个？如果是要mount具体某个GPU呢？
	router.GET("/addgpu/namespace/:namespace/pod/:pod/gpu/:gpuNum/isEntireMount/:isEntireMount", AddGPU)
	router.POST("/removegpu/namespace/:namespace/pod/:pod/force/:force", RemoveGPU)

	//创建service
	srv := &http.Server{
		Handler: router,
		Addr:    ":8080",
	}
	Logger.Info("Start gpu mounter master on " + srv.Addr)

	//监听http请求
	err := srv.ListenAndServe()
	if err != nil {
		Logger.Error("Failed to start gpu mounter master")
		Logger.Error(err)
		return
	}
}

// 从k8s集群中获取所有 label为gpu-mounter-worker节点的 pod
func findAllWorker() (map[string]corev1.Pod, error) {
	clientSet, err := config.GetClientSet()
	if err != nil {
		Logger.Error("Connect to k8s failed")
		Logger.Error(err.Error())
		return nil, err
	}

	//通过k8s接口获取指定label的Pod
	podList, err := clientSet.CoreV1().Pods("kube-system").List(context.TODO(), metav1.ListOptions{
		LabelSelector: "app=gpu-mounter-worker",
	})
	if err != nil {
		Logger.Error("Failed to gpu mounter worker")
		return nil, err
	}

	//podCache
	workerMap := make(map[string]corev1.Pod)
	for _, worker := range podList.Items {
		workerMap[worker.Spec.NodeName] = worker
		Logger.Info("Worker: ", worker.Name, " Node: ", worker.Spec.NodeName)
	}
	return workerMap, nil
}
