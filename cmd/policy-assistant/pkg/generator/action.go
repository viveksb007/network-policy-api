package generator

import networkingv1 "k8s.io/api/networking/v1"

// Action models a sum type (discriminated union): exactly one field must be non-null.
type Action struct {
	CreatePolicy *CreatePolicyAction
	UpdatePolicy *UpdatePolicyAction
	DeletePolicy *DeletePolicyAction

	CreateNamespace    *CreateNamespaceAction
	SetNamespaceLabels *SetNamespaceLabelsAction
	DeleteNamespace    *DeleteNamespaceAction

	ReadNetworkPolicies *ReadNetworkPoliciesAction

	CreatePod    *CreatePodAction
	SetPodLabels *SetPodLabelsAction
	DeletePod    *DeletePodAction
}

type CreatePolicyAction struct {
	Policy *networkingv1.NetworkPolicy
}

func CreatePolicy(policy *networkingv1.NetworkPolicy) *Action {
	return &Action{CreatePolicy: &CreatePolicyAction{Policy: policy}}
}

type UpdatePolicyAction struct {
	Policy *networkingv1.NetworkPolicy
}

func UpdatePolicy(policy *networkingv1.NetworkPolicy) *Action {
	return &Action{UpdatePolicy: &UpdatePolicyAction{Policy: policy}}
}

type DeletePolicyAction struct {
	Namespace string
	Name      string
}

func DeletePolicy(ns string, name string) *Action {
	return &Action{DeletePolicy: &DeletePolicyAction{Namespace: ns, Name: name}}
}

type CreateNamespaceAction struct {
	Namespace string
	Labels    map[string]string
}

func CreateNamespace(ns string, labels map[string]string) *Action {
	return &Action{CreateNamespace: &CreateNamespaceAction{Namespace: ns, Labels: labels}}
}

type SetNamespaceLabelsAction struct {
	Namespace string
	Labels    map[string]string
}

func SetNamespaceLabels(ns string, labels map[string]string) *Action {
	return &Action{SetNamespaceLabels: &SetNamespaceLabelsAction{Namespace: ns, Labels: labels}}
}

type DeleteNamespaceAction struct {
	Namespace string
}

func DeleteNamespace(ns string) *Action {
	return &Action{DeleteNamespace: &DeleteNamespaceAction{Namespace: ns}}
}

type ReadNetworkPoliciesAction struct {
	Namespaces []string
}

func ReadNetworkPolicies(namespaces []string) *Action {
	return &Action{ReadNetworkPolicies: &ReadNetworkPoliciesAction{Namespaces: namespaces}}
}

type CreatePodAction struct {
	Namespace string
	Pod       string
	Labels    map[string]string
}

func CreatePod(namespace string, pod string, labels map[string]string) *Action {
	return &Action{CreatePod: &CreatePodAction{
		Namespace: namespace,
		Pod:       pod,
		Labels:    labels,
	}}
}

type SetPodLabelsAction struct {
	Namespace string
	Pod       string
	Labels    map[string]string
}

func SetPodLabels(namespace string, pod string, labels map[string]string) *Action {
	return &Action{SetPodLabels: &SetPodLabelsAction{
		Namespace: namespace,
		Pod:       pod,
		Labels:    labels,
	}}
}

type DeletePodAction struct {
	Namespace string
	Pod       string
}

func DeletePod(namespace string, pod string) *Action {
	return &Action{DeletePod: &DeletePodAction{
		Namespace: namespace,
		Pod:       pod,
	}}
}

// RemapNamespaces returns a copy of the Action with all namespace name references
// transformed by the remap function. Label values are not remapped since policies
// match on labels, not namespace names.
func (a *Action) RemapNamespaces(remap func(string) string) *Action {
	switch {
	case a.CreatePolicy != nil:
		policy := a.CreatePolicy.Policy.DeepCopy()
		policy.Namespace = remap(policy.Namespace)
		return &Action{CreatePolicy: &CreatePolicyAction{Policy: policy}}
	case a.UpdatePolicy != nil:
		policy := a.UpdatePolicy.Policy.DeepCopy()
		policy.Namespace = remap(policy.Namespace)
		return &Action{UpdatePolicy: &UpdatePolicyAction{Policy: policy}}
	case a.DeletePolicy != nil:
		return &Action{DeletePolicy: &DeletePolicyAction{
			Namespace: remap(a.DeletePolicy.Namespace),
			Name:      a.DeletePolicy.Name,
		}}
	case a.CreateNamespace != nil:
		return &Action{CreateNamespace: &CreateNamespaceAction{
			Namespace: remap(a.CreateNamespace.Namespace),
			Labels:    a.CreateNamespace.Labels,
		}}
	case a.SetNamespaceLabels != nil:
		return &Action{SetNamespaceLabels: &SetNamespaceLabelsAction{
			Namespace: remap(a.SetNamespaceLabels.Namespace),
			Labels:    a.SetNamespaceLabels.Labels,
		}}
	case a.DeleteNamespace != nil:
		return &Action{DeleteNamespace: &DeleteNamespaceAction{
			Namespace: remap(a.DeleteNamespace.Namespace),
		}}
	case a.ReadNetworkPolicies != nil:
		newNs := make([]string, len(a.ReadNetworkPolicies.Namespaces))
		for i, ns := range a.ReadNetworkPolicies.Namespaces {
			newNs[i] = remap(ns)
		}
		return &Action{ReadNetworkPolicies: &ReadNetworkPoliciesAction{Namespaces: newNs}}
	case a.CreatePod != nil:
		return &Action{CreatePod: &CreatePodAction{
			Namespace: remap(a.CreatePod.Namespace),
			Pod:       a.CreatePod.Pod,
			Labels:    a.CreatePod.Labels,
		}}
	case a.SetPodLabels != nil:
		return &Action{SetPodLabels: &SetPodLabelsAction{
			Namespace: remap(a.SetPodLabels.Namespace),
			Pod:       a.SetPodLabels.Pod,
			Labels:    a.SetPodLabels.Labels,
		}}
	case a.DeletePod != nil:
		return &Action{DeletePod: &DeletePodAction{
			Namespace: remap(a.DeletePod.Namespace),
			Pod:       a.DeletePod.Pod,
		}}
	default:
		panic("invalid Action: no field set")
	}
}
