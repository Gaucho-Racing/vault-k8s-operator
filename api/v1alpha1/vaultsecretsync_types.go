package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type VaultSecretSyncSpec struct {
	VaultURL           string                `json:"vaultURL,omitempty"`
	ServiceAccountName string                `json:"serviceAccountName,omitempty"`
	Target             VaultSecretSyncTarget `json:"target"`
	Secrets            map[string]string     `json:"secrets"`
	RolloutTargets     []RolloutTarget       `json:"rolloutTargets,omitempty"`
	RefreshInterval    *metav1.Duration      `json:"refreshInterval,omitempty"`
}

type VaultSecretSyncTarget struct {
	Name string            `json:"name,omitempty"`
	Type corev1.SecretType `json:"type,omitempty"`
}

type RolloutTarget struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

type VaultSecretSyncStatus struct {
	ObservedGeneration    int64              `json:"observedGeneration,omitempty"`
	SecretName            string             `json:"secretName,omitempty"`
	SecretResourceVersion string             `json:"secretResourceVersion,omitempty"`
	SyncedAt              *metav1.Time       `json:"syncedAt,omitempty"`
	Conditions            []metav1.Condition `json:"conditions,omitempty"`
}

type VaultSecretSync struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VaultSecretSyncSpec   `json:"spec,omitempty"`
	Status VaultSecretSyncStatus `json:"status,omitempty"`
}

type VaultSecretSyncList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VaultSecretSync `json:"items"`
}

func init() {
	SchemeBuilder.Register(&VaultSecretSync{}, &VaultSecretSyncList{})
}
