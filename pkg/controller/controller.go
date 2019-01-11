package controller

import (
	"fmt"
	"log"
	"time"

	"github.com/stakater/IngressMonitorController/pkg/config"
	"github.com/stakater/IngressMonitorController/pkg/kube/wrappers"
	"github.com/stakater/IngressMonitorController/pkg/models"
	"github.com/stakater/IngressMonitorController/pkg/monitors"
	"github.com/stakater/IngressMonitorController/pkg/util"
	"k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"strconv"
)

// MonitorController which can be used for monitoring ingresses
type MonitorController struct {
	kubeClient      kubernetes.Interface
	namespace       string
	indexer         cache.Indexer
	queue           workqueue.RateLimitingInterface
	informer        cache.Controller
	monitorServices []monitors.MonitorServiceProxy
	config          config.Config
}

func NewMonitorController(namespace string, kubeClient kubernetes.Interface, config config.Config) *MonitorController {
	controller := &MonitorController{
		kubeClient: kubeClient,
		namespace:  namespace,
		config:     config,
	}

	controller.monitorServices = setupMonitorServicesForProviders(config.Providers)

	queue := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())

	// Create the Ingress Watcher
	ingressListWatcher := cache.NewListWatchFromClient(kubeClient.ExtensionsV1beta1().RESTClient(), "ingresses", namespace, fields.Everything())

	indexer, informer := cache.NewIndexerInformer(ingressListWatcher, &v1beta1.Ingress{}, 0, cache.ResourceEventHandlerFuncs{
		AddFunc:    controller.onIngressAdded,
		UpdateFunc: controller.onIngressUpdated,
		DeleteFunc: controller.onIngressDeleted,
	}, cache.Indexers{})
	controller.indexer = indexer
	controller.informer = informer
	controller.queue = queue

	return controller
}

func setupMonitorServicesForProviders(providers []config.Provider) []monitors.MonitorServiceProxy {
	if len(providers) < 1 {
		log.Panic("Cannot Instantiate controller with no providers")
	}

	monitorServices := []monitors.MonitorServiceProxy{}

	for index := 0; index < len(providers); index++ {
		monitorServices = append(monitorServices, monitors.CreateMonitorService(&providers[index]))
	}

	return monitorServices
}

func (c *MonitorController) Run(threadiness int, stopCh chan struct{}) {
	defer runtime.HandleCrash()

	// Let the workers stop when we are done
	defer c.queue.ShutDown()
	log.Println("Starting Ingress Monitor controller")

	go c.informer.Run(stopCh)

	// Wait for all involved caches to be synced, before processing items from the queue is started
	if !cache.WaitForCacheSync(stopCh, c.informer.HasSynced) {
		runtime.HandleError(fmt.Errorf("Timed out waiting for caches to sync"))
		return
	}

	for i := 0; i < threadiness; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}

	<-stopCh
	log.Println("Stopping Ingress Monitor controller")
}

func (c *MonitorController) runWorker() {
	for c.processNextItem() {
	}
}

func (c *MonitorController) processNextItem() bool {
	// Wait until there is a new item in the working queue
	ingressAction, quit := c.queue.Get()
	if quit {
		return false
	}
	// Tell the queue that we are done with processing this key. This unblocks the key for other workers
	// This allows safe parallel processing because two ingresses with the same key are never processed in
	// parallel.
	defer c.queue.Done(ingressAction)

	// Invoke the method containing the business logic
	err := ingressAction.(IngressAction).handle(c)
	// Handle the error if something went wrong during the execution of the business logic
	c.handleErr(err, ingressAction)
	return true
}

func (c *MonitorController) getMonitorName(ingress *v1beta1.Ingress) string {
	annotations := ingress.GetAnnotations()
	if value, ok := annotations[wrappers.MonitorNameAnnotation]; ok {
		return value
	}

	format, err := util.GetNameTemplateFormat(c.config.MonitorNameTemplate)
	if err != nil {
		log.Fatal("Failed to parse MonitorNameTemplate")
	}
	return fmt.Sprintf(format, ingress.GetName(), ingress.GetNamespace())
}

func (c *MonitorController) getMonitorURL(ingress *v1beta1.Ingress) string {
	ingressWrapper := wrappers.IngressWrapper{
		Ingress:    ingress,
		Namespace:  ingress.Namespace,
		KubeClient: c.kubeClient,
	}

	return ingressWrapper.GetURL()
}

func (c *MonitorController) removeMonitorsIfExist(monitorName string) {
	for index := 0; index < len(c.monitorServices); index++ {
		c.removeMonitorIfExists(c.monitorServices[index], monitorName)
	}
}

func (c *MonitorController) removeMonitorIfExists(monitorService monitors.MonitorServiceProxy, monitorName string) {
	m, _ := monitorService.GetByName(monitorName)

	if m != nil { // Monitor Exists
		monitorService.Remove(*m) // Remove the monitor
	} else {
		log.Println("Cannot find monitor with name: " + monitorName)
	}
}

func (c *MonitorController) createOrUpdateMonitors(monitorName string, oldMonitorName string, monitorURL string, annotations map[string]string) {
	for index := 0; index < len(c.monitorServices); index++ {
		monitorService := c.monitorServices[index]
		c.createOrUpdateMonitor(monitorService, monitorName, oldMonitorName, monitorURL, annotations)
	}
}

func (c *MonitorController) createOrUpdateMonitor(monitorService monitors.MonitorServiceProxy, monitorName string, oldMonitorName string, monitorURL string, annotations map[string]string) {
	m, _ := monitorService.GetByName(oldMonitorName)

	if m != nil { // Monitor Already Exists
		log.Println("Monitor already exists for ingress: " + monitorName + "; interval: " + strconv.Itoa(m.Interval) + ", " + strconv.FormatBool(m.Interval == 300))
		if m.URL != monitorURL || monitorName != oldMonitorName || m.Interval == 300 {
			m.URL = monitorURL
			m.Name = monitorName
			m.Annotations = annotations
			monitorService.Update(*m)
		}
	} else {
		// Create a new monitor for this ingress
		m := models.Monitor{
			Name:        monitorName,
			URL:         monitorURL,
			Annotations: annotations,
		}
		monitorService.Add(m)
	}
}

// handleErr checks if an error happened and makes sure we will retry later.
func (c *MonitorController) handleErr(err error, key interface{}) {
	if err == nil {
		// Forget about the #AddRateLimited history of the key on every successful synchronization.
		// This ensures that future processing of updates for this key is not delayed because of
		// an outdated error history.
		c.queue.Forget(key)
		return
	}

	// This controller retries 5 times if something goes wrong. After that, it stops trying.
	if c.queue.NumRequeues(key) < 5 {
		log.Printf("Error syncing ingress %v: %v", key, err)

		// Re-enqueue the key rate limited. Based on the rate limiter on the
		// queue and the re-enqueue history, the key will be processed later again.
		c.queue.AddRateLimited(key)
		return
	}

	c.queue.Forget(key)
	// Report to an external entity that, even after several retries, we could not successfully process this key
	runtime.HandleError(err)
	log.Printf("Dropping ingress %q out of the queue: %v", key, err)
}

func (c *MonitorController) onIngressAdded(obj interface{}) {
	c.queue.Add(IngressUpdatedAction{
		ingress: obj.(*v1beta1.Ingress),
	})
}

func (c *MonitorController) onIngressUpdated(old interface{}, new interface{}) {
	c.queue.Add(IngressUpdatedAction{
		ingress:    new.(*v1beta1.Ingress),
		oldIngress: old.(*v1beta1.Ingress),
	})
}

func (c *MonitorController) onIngressDeleted(obj interface{}) {
	c.queue.Add(IngressDeletedAction{
		ingress: obj.(*v1beta1.Ingress),
	})
}
