package controller

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	vaultv1alpha1 "github.com/gaucho-racing/vault-k8s-operator/api/v1alpha1"
	"github.com/gaucho-racing/vault-k8s-operator/internal/vault"
	appsv1 "k8s.io/api/apps/v1"
	authv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	meta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const syncedCondition = "Synced"
const rolloutHashAnnotationPrefix = "vault.gauchoracing.com/secret-hash-"

type VaultSecretSyncReconciler struct {
	client.Client
	Reader                 client.Reader
	Scheme                 *runtime.Scheme
	VaultClient            *vault.Client
	DefaultVaultURL        string
	Audience               string
	DefaultRefreshInterval time.Duration
}

func (r *VaultSecretSyncReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)

	var sync vaultv1alpha1.VaultSecretSync
	if err := r.Get(ctx, req.NamespacedName, &sync); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	refreshInterval := r.refreshInterval(sync)
	if err := r.reconcile(ctx, &sync); err != nil {
		log.Error(err, "failed to sync vault secret")
		r.setSyncedCondition(&sync, metav1.ConditionFalse, "SyncFailed", err.Error())
		_ = r.Status().Update(ctx, &sync)
		return ctrl.Result{RequeueAfter: refreshInterval}, err
	}

	log.Info("synced vault secret",
		"secret", sync.Status.SecretName,
		"keys", len(sync.Spec.Secrets),
		"nextSyncIn", refreshInterval)
	return ctrl.Result{RequeueAfter: refreshInterval}, nil
}

func (r *VaultSecretSyncReconciler) reconcile(ctx context.Context, sync *vaultv1alpha1.VaultSecretSync) error {
	vaultURL := sync.Spec.VaultURL
	if vaultURL == "" {
		vaultURL = r.DefaultVaultURL
	}
	if vaultURL == "" {
		return fmt.Errorf("vault URL is required")
	}

	token, err := r.serviceAccountToken(ctx, sync)
	if err != nil {
		return err
	}

	vaultClient := r.VaultClient
	if vaultClient == nil {
		vaultClient = vault.NewClient()
	}
	values, err := vaultClient.PullSecrets(ctx, vaultURL, token, sync.Spec.Secrets)
	if err != nil {
		return err
	}

	secret, err := r.applySecret(ctx, sync, values)
	if err != nil {
		return err
	}

	if err := r.rolloutTargets(ctx, sync, secretDataHash(secret.Data)); err != nil {
		return err
	}

	now := metav1.Now()
	sync.Status.ObservedGeneration = sync.Generation
	sync.Status.SecretName = secret.Name
	sync.Status.SecretResourceVersion = secret.ResourceVersion
	sync.Status.SyncedAt = &now
	r.setSyncedCondition(sync, metav1.ConditionTrue, "Synced", "Secret synced from Vault")
	return r.Status().Update(ctx, sync)
}

func (r *VaultSecretSyncReconciler) serviceAccountToken(ctx context.Context, sync *vaultv1alpha1.VaultSecretSync) (string, error) {
	serviceAccountName := sync.Spec.ServiceAccountName
	if serviceAccountName == "" {
		serviceAccountName = "default"
	}

	serviceAccount := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceAccountName,
			Namespace: sync.Namespace,
		},
	}
	tokenRequest := &authv1.TokenRequest{
		Spec: authv1.TokenRequestSpec{
			Audiences:         []string{r.Audience},
			ExpirationSeconds: ptr.To[int64](600),
		},
	}
	if err := r.SubResource("token").Create(ctx, serviceAccount, tokenRequest); err != nil {
		return "", fmt.Errorf("create service account token: %w", err)
	}
	if tokenRequest.Status.Token == "" {
		return "", fmt.Errorf("service account token response did not include a token")
	}
	return tokenRequest.Status.Token, nil
}

func (r *VaultSecretSyncReconciler) applySecret(ctx context.Context, sync *vaultv1alpha1.VaultSecretSync, values map[string]string) (*corev1.Secret, error) {
	name := sync.Spec.Target.Name
	if name == "" {
		name = sync.Name
	}
	secretType := sync.Spec.Target.Type
	if secretType == "" {
		secretType = corev1.SecretTypeOpaque
	}

	data := make(map[string][]byte, len(values))
	for key, value := range values {
		data[key] = []byte(value)
	}

	secret := &corev1.Secret{}
	err := r.Get(ctx, client.ObjectKey{Namespace: sync.Namespace, Name: name}, secret)
	if apierrors.IsNotFound(err) {
		secret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: sync.Namespace,
				Labels: map[string]string{
					"app.kubernetes.io/managed-by":             "vault-k8s-operator",
					"vault.gauchoracing.com/vault-secret-sync": sync.Name,
				},
			},
			Type: secretType,
			Data: data,
		}
		if err := controllerutil.SetControllerReference(sync, secret, r.Scheme); err != nil {
			return nil, err
		}
		return secret, r.Create(ctx, secret)
	}
	if err != nil {
		return nil, err
	}

	original := secret.DeepCopy()
	secret.Type = secretType
	secret.Data = data
	if secret.Labels == nil {
		secret.Labels = map[string]string{}
	}
	secret.Labels["app.kubernetes.io/managed-by"] = "vault-k8s-operator"
	secret.Labels["vault.gauchoracing.com/vault-secret-sync"] = sync.Name
	if err := controllerutil.SetControllerReference(sync, secret, r.Scheme); err != nil {
		return nil, err
	}
	if !secretChanged(original, secret) {
		return secret, nil
	}
	return secret, r.Update(ctx, secret)
}

func (r *VaultSecretSyncReconciler) rolloutTargets(ctx context.Context, sync *vaultv1alpha1.VaultSecretSync, secretHash string) error {
	if len(sync.Spec.RolloutTargets) == 0 {
		return nil
	}

	reader := r.Reader
	if reader == nil {
		reader = r.Client
	}
	annotationKey := rolloutHashAnnotationKey(sync)
	for _, target := range sync.Spec.RolloutTargets {
		kind, err := normalizedRolloutTargetKind(target.Kind)
		if err != nil {
			return err
		}
		name := strings.TrimSpace(target.Name)
		if name == "" {
			return fmt.Errorf("rollout target name is required")
		}

		key := client.ObjectKey{Namespace: sync.Namespace, Name: name}
		switch kind {
		case "Deployment":
			workload := &appsv1.Deployment{}
			if err := reader.Get(ctx, key, workload); err != nil {
				return fmt.Errorf("get rollout target Deployment/%s: %w", name, err)
			}
			if err := r.patchPodTemplateHash(ctx, workload, annotationKey, secretHash); err != nil {
				return fmt.Errorf("patch rollout target Deployment/%s: %w", name, err)
			}
		case "StatefulSet":
			workload := &appsv1.StatefulSet{}
			if err := reader.Get(ctx, key, workload); err != nil {
				return fmt.Errorf("get rollout target StatefulSet/%s: %w", name, err)
			}
			if err := r.patchPodTemplateHash(ctx, workload, annotationKey, secretHash); err != nil {
				return fmt.Errorf("patch rollout target StatefulSet/%s: %w", name, err)
			}
		case "DaemonSet":
			workload := &appsv1.DaemonSet{}
			if err := reader.Get(ctx, key, workload); err != nil {
				return fmt.Errorf("get rollout target DaemonSet/%s: %w", name, err)
			}
			if err := r.patchPodTemplateHash(ctx, workload, annotationKey, secretHash); err != nil {
				return fmt.Errorf("patch rollout target DaemonSet/%s: %w", name, err)
			}
		}
	}
	return nil
}

type podTemplateHashTarget interface {
	client.Object
	DeepCopyObject() runtime.Object
}

func (r *VaultSecretSyncReconciler) patchPodTemplateHash(ctx context.Context, workload podTemplateHashTarget, annotationKey string, secretHash string) error {
	annotations := podTemplateAnnotations(workload)
	if annotations[annotationKey] == secretHash {
		return nil
	}

	original := workload.DeepCopyObject().(client.Object)
	setPodTemplateAnnotation(workload, annotationKey, secretHash)
	return r.Patch(ctx, workload, client.MergeFrom(original))
}

func podTemplateAnnotations(workload podTemplateHashTarget) map[string]string {
	switch typed := workload.(type) {
	case *appsv1.Deployment:
		return typed.Spec.Template.Annotations
	case *appsv1.StatefulSet:
		return typed.Spec.Template.Annotations
	case *appsv1.DaemonSet:
		return typed.Spec.Template.Annotations
	default:
		return nil
	}
}

func setPodTemplateAnnotation(workload podTemplateHashTarget, key string, value string) {
	switch typed := workload.(type) {
	case *appsv1.Deployment:
		if typed.Spec.Template.Annotations == nil {
			typed.Spec.Template.Annotations = map[string]string{}
		}
		typed.Spec.Template.Annotations[key] = value
	case *appsv1.StatefulSet:
		if typed.Spec.Template.Annotations == nil {
			typed.Spec.Template.Annotations = map[string]string{}
		}
		typed.Spec.Template.Annotations[key] = value
	case *appsv1.DaemonSet:
		if typed.Spec.Template.Annotations == nil {
			typed.Spec.Template.Annotations = map[string]string{}
		}
		typed.Spec.Template.Annotations[key] = value
	}
}

func normalizedRolloutTargetKind(kind string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "deployment", "deployments":
		return "Deployment", nil
	case "statefulset", "statefulsets":
		return "StatefulSet", nil
	case "daemonset", "daemonsets":
		return "DaemonSet", nil
	default:
		return "", fmt.Errorf("unsupported rollout target kind %q", kind)
	}
}

func rolloutHashAnnotationKey(sync *vaultv1alpha1.VaultSecretSync) string {
	sum := sha256.Sum256([]byte(sync.Namespace + "/" + sync.Name))
	return rolloutHashAnnotationPrefix + hex.EncodeToString(sum[:])[:12]
}

func secretDataHash(data map[string][]byte) string {
	keys := make([]string, 0, len(data))
	for key := range data {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	hash := sha256.New()
	for _, key := range keys {
		hash.Write([]byte(key))
		hash.Write([]byte{0})
		hash.Write(data[key])
		hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func secretDataEqual(left map[string][]byte, right map[string][]byte) bool {
	if len(left) != len(right) {
		return false
	}
	for key, leftValue := range left {
		rightValue, ok := right[key]
		if !ok || !bytes.Equal(leftValue, rightValue) {
			return false
		}
	}
	return true
}

func secretChanged(original *corev1.Secret, updated *corev1.Secret) bool {
	return original.Type != updated.Type ||
		!secretDataEqual(original.Data, updated.Data) ||
		!reflect.DeepEqual(original.Labels, updated.Labels) ||
		!reflect.DeepEqual(original.OwnerReferences, updated.OwnerReferences)
}

func (r *VaultSecretSyncReconciler) refreshInterval(sync vaultv1alpha1.VaultSecretSync) time.Duration {
	if sync.Spec.RefreshInterval != nil && sync.Spec.RefreshInterval.Duration > 0 {
		return sync.Spec.RefreshInterval.Duration
	}
	if r.DefaultRefreshInterval > 0 {
		return r.DefaultRefreshInterval
	}
	return 5 * time.Minute
}

func (r *VaultSecretSyncReconciler) setSyncedCondition(sync *vaultv1alpha1.VaultSecretSync, status metav1.ConditionStatus, reason string, message string) {
	condition := metav1.Condition{
		Type:               syncedCondition,
		Status:             status,
		ObservedGeneration: sync.Generation,
		LastTransitionTime: metav1.Now(),
		Reason:             reason,
		Message:            message,
	}
	meta.SetStatusCondition(&sync.Status.Conditions, condition)
}

func (r *VaultSecretSyncReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&vaultv1alpha1.VaultSecretSync{}).
		Owns(&corev1.Secret{}).
		Named("vaultsecretsync").
		Complete(r)
}
