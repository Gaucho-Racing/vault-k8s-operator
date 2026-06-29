package v1alpha1

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func (in *VaultSecretSync) DeepCopyInto(out *VaultSecretSync) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

func (in *VaultSecretSync) DeepCopy() *VaultSecretSync {
	if in == nil {
		return nil
	}
	out := new(VaultSecretSync)
	in.DeepCopyInto(out)
	return out
}

func (in *VaultSecretSync) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

func (in *VaultSecretSyncList) DeepCopyInto(out *VaultSecretSyncList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]VaultSecretSync, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

func (in *VaultSecretSyncList) DeepCopy() *VaultSecretSyncList {
	if in == nil {
		return nil
	}
	out := new(VaultSecretSyncList)
	in.DeepCopyInto(out)
	return out
}

func (in *VaultSecretSyncList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

func (in *VaultSecretSyncSpec) DeepCopyInto(out *VaultSecretSyncSpec) {
	*out = *in
	out.Target = in.Target
	if in.Secrets != nil {
		in, out := &in.Secrets, &out.Secrets
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	if in.RefreshInterval != nil {
		in, out := &in.RefreshInterval, &out.RefreshInterval
		*out = new(v1.Duration)
		**out = **in
	}
}

func (in *VaultSecretSyncSpec) DeepCopy() *VaultSecretSyncSpec {
	if in == nil {
		return nil
	}
	out := new(VaultSecretSyncSpec)
	in.DeepCopyInto(out)
	return out
}

func (in *VaultSecretSyncStatus) DeepCopyInto(out *VaultSecretSyncStatus) {
	*out = *in
	if in.SyncedAt != nil {
		in, out := &in.SyncedAt, &out.SyncedAt
		*out = new(v1.Time)
		**out = **in
	}
	if in.Conditions != nil {
		in, out := &in.Conditions, &out.Conditions
		*out = make([]v1.Condition, len(*in))
		copy(*out, *in)
	}
}

func (in *VaultSecretSyncStatus) DeepCopy() *VaultSecretSyncStatus {
	if in == nil {
		return nil
	}
	out := new(VaultSecretSyncStatus)
	in.DeepCopyInto(out)
	return out
}
