/*
Copyright 2025.

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
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	mydomainv1 "github.com/huonguyenlt/secret-manager/api/v1"
)

// SecretManagerReconciler reconciles a SecretManager object
type SecretManagerReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=my.domain,resources=secretmanagers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=my.domain,resources=secretmanagers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=my.domain,resources=secretmanagers/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secret,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the SecretManager object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.22.4/pkg/reconcile
func (r *SecretManagerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var sm mydomainv1.SecretManager
	if err := r.Get(ctx, req.NamespacedName, &sm); err != nil {
		// Ignore not-found errors, requeue on others
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Load AWS config with region
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion("ap-southeast-1"))
	if err != nil {
		log.Error(err, "unable to load AWS config")
		return ctrl.Result{}, err
	}

	// Create AWS Secrets Manager client
	svc := secretsmanager.NewFromConfig(cfg)

	// Get the secret value from AWS
	awsSecretName := sm.Spec.SourceSecretName
	getOut, err := svc.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{
		SecretId: aws.String(awsSecretName),
	})
	if err != nil {
		log.Error(err, fmt.Sprintf("failed to get secret %s from AWS", awsSecretName))
		return ctrl.Result{}, err
	}

	// Parse the secret string as JSON to map[string][]byte for K8s Secret
	secretData := map[string][]byte{}
	if getOut.SecretString != nil {
		var tmp map[string]string
		if err := json.Unmarshal([]byte(*getOut.SecretString), &tmp); err != nil {
			log.Error(err, "failed to unmarshal AWS secret string")
			return ctrl.Result{}, err
		}
		for k, v := range tmp {
			secretData[k] = []byte(v)
		}
	} else if getOut.SecretBinary != nil {
		// Optionally handle binary secrets
		secretData["secret"] = getOut.SecretBinary
	}

	// Create or update the Kubernetes Secret
	k8sSecret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      sm.Spec.Name,
			Namespace: sm.Namespace,
		},
		Data: secretData,
		Type: v1.SecretTypeOpaque,
	}

	// Set owner reference for garbage collection
	if err := ctrl.SetControllerReference(&sm, k8sSecret, r.Scheme); err != nil {
		log.Error(err, "failed to set owner reference on secret")
		return ctrl.Result{}, err
	}

	// Try to create or update the secret
	var existingSecret v1.Secret
	err = r.Get(ctx, client.ObjectKey{Name: k8sSecret.Name, Namespace: k8sSecret.Namespace}, &existingSecret)
	if err == nil {
		// Secret exists, only update if data has changed
		needUpdate := false
		if len(existingSecret.Data) != len(k8sSecret.Data) {
			needUpdate = true
		} else {
			for k, v := range k8sSecret.Data {
				if ev, ok := existingSecret.Data[k]; !ok || string(ev) != string(v) {
					needUpdate = true
					break
				}
			}
		}
		if needUpdate {
			existingSecret.Data = k8sSecret.Data
			existingSecret.Type = k8sSecret.Type
			if err := r.Update(ctx, &existingSecret); err != nil {
				log.Error(err, "failed to update existing k8s secret")
				return ctrl.Result{}, err
			}
			log.Info(fmt.Sprintf("Updated Kubernetes secret %s", k8sSecret.Name))
		} else {
			log.Info(fmt.Sprintf("Kubernetes secret %s is up to date", k8sSecret.Name))
		}
	} else if client.IgnoreNotFound(err) == nil {
		// Secret does not exist, create it
		if err := r.Create(ctx, k8sSecret); err != nil {
			log.Error(err, "failed to create k8s secret")
			return ctrl.Result{}, err
		}
		log.Info(fmt.Sprintf("Created Kubernetes secret %s", k8sSecret.Name))
	} else {
		// Some other error
		log.Error(err, "failed to get k8s secret")
		return ctrl.Result{}, err
	}

	// At the end of the function, always requeue after a fixed interval
	return ctrl.Result{RequeueAfter: time.Second * 10}, nil // 10 seconds
}

// SetupWithManager sets up the controller with the Manager.
func (r *SecretManagerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&mydomainv1.SecretManager{}).
		Named("secretmanager").
		Complete(r)
}
