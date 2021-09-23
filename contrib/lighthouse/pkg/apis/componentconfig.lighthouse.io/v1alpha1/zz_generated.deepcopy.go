// +build !ignore_autogenerated

// Code generated by deepcopy-gen. DO NOT EDIT.

package v1alpha1

import (
	runtime "k8s.io/apimachinery/pkg/runtime"
)

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *HookConfiguration) DeepCopyInto(out *HookConfiguration) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	if in.WebHooks != nil {
		in, out := &in.WebHooks, &out.WebHooks
		*out = make(HookConfigurationList, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new HookConfiguration.
func (in *HookConfiguration) DeepCopy() *HookConfiguration {
	if in == nil {
		return nil
	}
	out := new(HookConfiguration)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *HookConfiguration) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *HookConfigurationItem) DeepCopyInto(out *HookConfigurationItem) {
	*out = *in
	if in.Stages != nil {
		in, out := &in.Stages, &out.Stages
		*out = make(HookStageList, len(*in))
		copy(*out, *in)
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new HookConfigurationItem.
func (in *HookConfigurationItem) DeepCopy() *HookConfigurationItem {
	if in == nil {
		return nil
	}
	out := new(HookConfigurationItem)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in HookConfigurationList) DeepCopyInto(out *HookConfigurationList) {
	{
		in := &in
		*out = make(HookConfigurationList, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
		return
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new HookConfigurationList.
func (in HookConfigurationList) DeepCopy() HookConfigurationList {
	if in == nil {
		return nil
	}
	out := new(HookConfigurationList)
	in.DeepCopyInto(out)
	return *out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *HookStage) DeepCopyInto(out *HookStage) {
	*out = *in
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new HookStage.
func (in *HookStage) DeepCopy() *HookStage {
	if in == nil {
		return nil
	}
	out := new(HookStage)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in HookStageList) DeepCopyInto(out *HookStageList) {
	{
		in := &in
		*out = make(HookStageList, len(*in))
		copy(*out, *in)
		return
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new HookStageList.
func (in HookStageList) DeepCopy() HookStageList {
	if in == nil {
		return nil
	}
	out := new(HookStageList)
	in.DeepCopyInto(out)
	return *out
}