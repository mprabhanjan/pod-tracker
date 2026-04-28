/*
Copyright 2026.

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

package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	crdv1 "github.com/mprabhanjan/pod-tracker/api/v1alpha1"
)

// PodTrackerReconciler reconciles a PodTracker object
type PodTrackerReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// Define the Google Chat message structure
type GChatMessage struct {
	Text string `json:"text"`
}

func reportGChat(reporter crdv1.PodTracker, pod corev1.Pod) {

	logger := logf.Log.WithName("gchat-reporter").WithValues("pod", pod.Name, "namespace", pod.Namespace)
	logger.V(1).Info("Initiating Google Chat report")

	// 1. Construct the message with Google Chat-supported Markdown
	message := fmt.Sprintf("🚨 *New Pod Detected*\n*Name:* `%s` \n*Namespace:* `%s` \n*Status:* %s",
		pod.Name, pod.Namespace, pod.Status.Phase)
	payload := GChatMessage{Text: message}

	// 2. Marshal to JSON
	bytesRepresentation, err := json.Marshal(payload)
	if err != nil {
		logger.Error(err, "Failed to marshal JSON payload")
		return
	}

	// 3. Determine Webhook URL (Fallback to your provided default)
	webhookURL := reporter.Spec.Report.Webhook
	if webhookURL == "" {
		// webhookURL = "https://abcd.efgh.ijlk"
		logger.Error(err, "Invalid webhookURL")
		return
	}

	// 4. Create the Request with a Context/Timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", webhookURL, bytes.NewBuffer(bytesRepresentation))
	if err != nil {
		logger.Error(err, "Failed to create HTTP request")
		return
	}

	// 5. CRITICAL: Set correct headers. Google Chat requires UTF-8 charset.
	req.Header.Set("Content-Type", "application/json; charset=UTF-8")

	// 6. Execute the Request
	http_client := &http.Client{}
	resp, err := http_client.Do(req)
	if err != nil {
		logger.Error(err, "Network error while sending to Google Chat")
		return
	}

	defer func() {
		_ = resp.Body.Close()
	}()

	// 7. Enhanced Error Handling: Read the body on failure to see WHY it failed
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		logger.Info("Google Chat rejected the request",
			"status", resp.Status,
			"webhookURL", webhookURL,
			"response_body", string(body))
	} else {
		logger.V(1).Info("Successfully reported to Google Chat")
	}
}

// +kubebuilder:rbac:groups=mahioperators.mahioperators.com,resources=podtrackers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=mahioperators.mahioperators.com,resources=podtrackers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=mahioperators.mahioperators.com,resources=podtrackers/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;update;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the PodTracker object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/reconcile
func (r *PodTrackerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)

	var podTrackerList crdv1.PodTrackerList

	if err := r.List(ctx, &podTrackerList); err != nil {
		logger.Error(err, "unable to fetch pod tracker list")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if len(podTrackerList.Items) == 0 {
		logger.V(1).Info("no pod trackers configured")
		return ctrl.Result{}, nil
	} else {
		var podObject corev1.Pod
		err := r.Get(context.Background(), req.NamespacedName, &podObject)
		if err != nil {
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}
		logger.V(1).Info("found repoter configured. sending report")
		reportGChat(podTrackerList.Items[0], podObject)
	}
	return ctrl.Result{}, nil
}

func (r *PodTrackerReconciler) HandlePodEvents(ctx context.Context, pod client.Object) []reconcile.Request {

	// if pod.GetNamespace() != "default" {
	//		return []reconcile.Request{}
	// }

	namesapcedName := types.NamespacedName{
		Namespace: pod.GetNamespace(),
		Name:      pod.GetName(),
	}

	var podObject corev1.Pod
	err := r.Get(ctx, namesapcedName, &podObject)

	if err != nil {
		return []reconcile.Request{}
	}

	if len(podObject.Annotations) == 0 {
		logf.Log.V(1).Info("No annotations set, so this pod is becoming a tracked one now", "pod", podObject.Name)
	} else if podObject.GetAnnotations()["exampleAnnotation"] == "crd.devops.toolbox" {
		logf.Log.V(1).Info("Found a managed pod, lets report it", "pod", podObject.Name)
	} else {
		return []reconcile.Request{}
	}

	podObject.SetAnnotations(map[string]string{
		"exampleAnnotation": "crd.devops.toolbox",
	})

	if err := r.Update(context.TODO(), &podObject); err != nil {
		logf.Log.V(1).Info("error trying to update pod", "err", err)
	}
	requests := []reconcile.Request{
		{NamespacedName: namesapcedName},
	}
	return requests
	// return []reconcile.Request{}
}

// SetupWithManager sets up the controller with the Manager.
func (r *PodTrackerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&crdv1.PodTracker{}).
		Watches(
			&corev1.Pod{},
			handler.EnqueueRequestsFromMapFunc(r.HandlePodEvents),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
		).
		Complete(r)
}
