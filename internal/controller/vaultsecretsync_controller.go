package controller

import (
	"context"
	"fmt"
	"time"

	vaultv1alpha1 "github.com/gaucho-racing/vault-k8s-operator/api/v1alpha1"
	"github.com/gaucho-racing/vault-k8s-operator/internal/vault"
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

type VaultSecretSyncReconciler struct {
	client.Client
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
	return secret, r.Update(ctx, secret)
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
