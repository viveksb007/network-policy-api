package generator

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("RemapNamespaces", func() {
	Describe("Action.RemapNamespaces", func() {
		remap := func(ns string) string { return ns + "-w1" }

		It("should remap CreatePolicy namespace", func() {
			policy := &networkingv1.NetworkPolicy{
				ObjectMeta: metav1.ObjectMeta{Name: "test-pol", Namespace: "x"},
				Spec: networkingv1.NetworkPolicySpec{
					PodSelector: metav1.LabelSelector{MatchLabels: map[string]string{"pod": "a"}},
					Ingress: []networkingv1.NetworkPolicyIngressRule{{
						From: []networkingv1.NetworkPolicyPeer{{
							NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"ns": "y"}},
						}},
					}},
					PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
				},
			}
			action := CreatePolicy(policy)
			remapped := action.RemapNamespaces(remap)

			// Namespace name should be remapped
			Expect(remapped.CreatePolicy.Policy.Namespace).To(Equal("x-w1"))
			// Policy name should not change
			Expect(remapped.CreatePolicy.Policy.Name).To(Equal("test-pol"))
			// Label selectors should NOT be remapped
			Expect(remapped.CreatePolicy.Policy.Spec.Ingress[0].From[0].NamespaceSelector.MatchLabels["ns"]).To(Equal("y"))
			// PodSelector should not change
			Expect(remapped.CreatePolicy.Policy.Spec.PodSelector.MatchLabels["pod"]).To(Equal("a"))
			// Original should be unchanged
			Expect(action.CreatePolicy.Policy.Namespace).To(Equal("x"))
		})

		It("should remap UpdatePolicy namespace", func() {
			policy := &networkingv1.NetworkPolicy{
				ObjectMeta: metav1.ObjectMeta{Name: "test-pol", Namespace: "x"},
			}
			action := UpdatePolicy(policy)
			remapped := action.RemapNamespaces(remap)

			Expect(remapped.UpdatePolicy.Policy.Namespace).To(Equal("x-w1"))
			Expect(action.UpdatePolicy.Policy.Namespace).To(Equal("x"))
		})

		It("should remap DeletePolicy namespace", func() {
			action := DeletePolicy("x", "test-pol")
			remapped := action.RemapNamespaces(remap)

			Expect(remapped.DeletePolicy.Namespace).To(Equal("x-w1"))
			Expect(remapped.DeletePolicy.Name).To(Equal("test-pol"))
			Expect(action.DeletePolicy.Namespace).To(Equal("x"))
		})

		It("should remap CreateNamespace but not labels", func() {
			action := CreateNamespace("y-2", map[string]string{"ns": "y"})
			remapped := action.RemapNamespaces(remap)

			Expect(remapped.CreateNamespace.Namespace).To(Equal("y-2-w1"))
			Expect(remapped.CreateNamespace.Labels).To(Equal(map[string]string{"ns": "y"}))
		})

		It("should remap SetNamespaceLabels namespace but not labels", func() {
			action := SetNamespaceLabels("y", map[string]string{"ns": "y", "extra": "val"})
			remapped := action.RemapNamespaces(remap)

			Expect(remapped.SetNamespaceLabels.Namespace).To(Equal("y-w1"))
			Expect(remapped.SetNamespaceLabels.Labels).To(Equal(map[string]string{"ns": "y", "extra": "val"}))
		})

		It("should remap DeleteNamespace", func() {
			action := DeleteNamespace("y-2")
			remapped := action.RemapNamespaces(remap)

			Expect(remapped.DeleteNamespace.Namespace).To(Equal("y-2-w1"))
		})

		It("should remap ReadNetworkPolicies namespaces", func() {
			action := ReadNetworkPolicies([]string{"x", "y", "z"})
			remapped := action.RemapNamespaces(remap)

			Expect(remapped.ReadNetworkPolicies.Namespaces).To(Equal([]string{"x-w1", "y-w1", "z-w1"}))
			// Original should be unchanged
			Expect(action.ReadNetworkPolicies.Namespaces).To(Equal([]string{"x", "y", "z"}))
		})

		It("should remap CreatePod namespace but not pod name or labels", func() {
			action := CreatePod("x", "d", map[string]string{"pod": "d"})
			remapped := action.RemapNamespaces(remap)

			Expect(remapped.CreatePod.Namespace).To(Equal("x-w1"))
			Expect(remapped.CreatePod.Pod).To(Equal("d"))
			Expect(remapped.CreatePod.Labels).To(Equal(map[string]string{"pod": "d"}))
		})

		It("should remap SetPodLabels namespace but not pod name or labels", func() {
			action := SetPodLabels("y", "b", map[string]string{"pod": "b", "new-label": "abc"})
			remapped := action.RemapNamespaces(remap)

			Expect(remapped.SetPodLabels.Namespace).To(Equal("y-w1"))
			Expect(remapped.SetPodLabels.Pod).To(Equal("b"))
			Expect(remapped.SetPodLabels.Labels).To(Equal(map[string]string{"pod": "b", "new-label": "abc"}))
		})

		It("should remap DeletePod namespace but not pod name", func() {
			action := DeletePod("x", "d")
			remapped := action.RemapNamespaces(remap)

			Expect(remapped.DeletePod.Namespace).To(Equal("x-w1"))
			Expect(remapped.DeletePod.Pod).To(Equal("d"))
		})
	})

	Describe("TestCase.RemapNamespaces", func() {
		It("should return original when suffix is empty", func() {
			tc := NewSingleStepTestCase("test", NewStringSet(TagIngress), ProbeAllAvailable,
				CreatePolicy(BuildPolicy().NetworkPolicy()))
			remapped := tc.RemapNamespaces("")

			Expect(remapped).To(BeIdenticalTo(tc))
		})

		It("should remap all actions in all steps", func() {
			tc := NewTestCase("multi-step test",
				NewStringSet(TagIngress, TagDenyAll),
				NewTestStep(ProbeAllAvailable,
					CreatePolicy(BuildPolicy(SetNamespace("x")).NetworkPolicy())),
				NewTestStep(ProbeAllAvailable,
					SetNamespaceLabels("y", map[string]string{"ns": "y", "new-ns": "qrs"})),
				NewTestStep(ProbeAllAvailable,
					SetNamespaceLabels("y", map[string]string{"ns": "y"})),
			)

			remapped := tc.RemapNamespaces("-w2")

			// Description and tags should be preserved
			Expect(remapped.Description).To(Equal("multi-step test"))
			Expect(remapped.Tags).To(Equal(tc.Tags))

			// Step count should match
			Expect(len(remapped.Steps)).To(Equal(3))

			// Step 1: policy namespace remapped
			Expect(remapped.Steps[0].Actions[0].CreatePolicy.Policy.Namespace).To(Equal("x-w2"))

			// Step 2: SetNamespaceLabels namespace remapped, labels unchanged
			Expect(remapped.Steps[1].Actions[0].SetNamespaceLabels.Namespace).To(Equal("y-w2"))
			Expect(remapped.Steps[1].Actions[0].SetNamespaceLabels.Labels).To(Equal(map[string]string{"ns": "y", "new-ns": "qrs"}))

			// Step 3: SetNamespaceLabels namespace remapped, labels unchanged
			Expect(remapped.Steps[2].Actions[0].SetNamespaceLabels.Namespace).To(Equal("y-w2"))
			Expect(remapped.Steps[2].Actions[0].SetNamespaceLabels.Labels).To(Equal(map[string]string{"ns": "y"}))

			// Probe config should be preserved (same pointer)
			Expect(remapped.Steps[0].Probe).To(BeIdenticalTo(ProbeAllAvailable))

			// Original should be unchanged
			Expect(tc.Steps[0].Actions[0].CreatePolicy.Policy.Namespace).To(Equal("x"))
			Expect(tc.Steps[1].Actions[0].SetNamespaceLabels.Namespace).To(Equal("y"))
		})

		It("should remap the action test cases correctly", func() {
			gen := NewTestCaseGenerator(true, "1.2.3.4", []string{"x", "y", "z"}, []string{}, []string{})
			actionCases := gen.ActionTestCases()

			for _, tc := range actionCases {
				remapped := tc.RemapNamespaces("-w1")

				// Every step's actions should have remapped namespaces
				for _, step := range remapped.Steps {
					for _, action := range step.Actions {
						switch {
						case action.CreatePolicy != nil:
							Expect(action.CreatePolicy.Policy.Namespace).To(HaveSuffix("-w1"))
						case action.UpdatePolicy != nil:
							Expect(action.UpdatePolicy.Policy.Namespace).To(HaveSuffix("-w1"))
						case action.DeletePolicy != nil:
							Expect(action.DeletePolicy.Namespace).To(HaveSuffix("-w1"))
						case action.CreateNamespace != nil:
							Expect(action.CreateNamespace.Namespace).To(HaveSuffix("-w1"))
						case action.SetNamespaceLabels != nil:
							Expect(action.SetNamespaceLabels.Namespace).To(HaveSuffix("-w1"))
						case action.DeleteNamespace != nil:
							Expect(action.DeleteNamespace.Namespace).To(HaveSuffix("-w1"))
						case action.CreatePod != nil:
							Expect(action.CreatePod.Namespace).To(HaveSuffix("-w1"))
						case action.SetPodLabels != nil:
							Expect(action.SetPodLabels.Namespace).To(HaveSuffix("-w1"))
						case action.DeletePod != nil:
							Expect(action.DeletePod.Namespace).To(HaveSuffix("-w1"))
						}
					}
				}
			}
		})

		It("should not modify label values when remapping", func() {
			gen := NewTestCaseGenerator(true, "1.2.3.4", []string{"x", "y", "z"}, []string{}, []string{})
			allCases := gen.GenerateAllTestCases()

			for _, tc := range allCases {
				remapped := tc.RemapNamespaces("-w1")
				for si, step := range remapped.Steps {
					for ai, action := range step.Actions {
						if action.CreatePolicy != nil {
							orig := tc.Steps[si].Actions[ai].CreatePolicy.Policy
							// All label selectors in the policy spec should be identical
							Expect(action.CreatePolicy.Policy.Spec.PodSelector).To(Equal(orig.Spec.PodSelector))
							for ii, rule := range action.CreatePolicy.Policy.Spec.Ingress {
								for pi, peer := range rule.From {
									if peer.NamespaceSelector != nil {
										Expect(peer.NamespaceSelector).To(Equal(orig.Spec.Ingress[ii].From[pi].NamespaceSelector))
									}
								}
							}
						}
					}
				}
			}
		})
	})
})
