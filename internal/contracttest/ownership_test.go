package contracttest

import "testing"

type ownerReference struct {
	Group      string `json:"group"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	UID        string `json:"uid"`
	Controller bool   `json:"controller"`
}

func ownerAssociation(child, owner identity, refs []ownerReference, annotation string) association {
	if !child.valid() || !owner.valid() {
		return association{State: "invalid"}
	}
	if child.Origin != owner.Origin || child.Namespace != owner.Namespace {
		return association{State: "foreign"}
	}
	controllers := []ownerReference{}
	for _, ref := range refs {
		if ref.Controller {
			controllers = append(controllers, ref)
		}
	}
	if len(controllers) != 1 {
		return association{State: "unconfirmed"}
	}
	ref := controllers[0]
	if ref.UID != owner.UID {
		if annotation == owner.UID {
			return association{State: "conflict"}
		}
		return association{State: "foreign"}
	}
	if ref.Group != owner.Group || ref.Kind != owner.Kind || ref.Name != owner.Name || (annotation != "" && annotation != owner.UID) {
		return association{State: "conflict"}
	}
	return association{State: "confirmed", Basis: "metadata.ownerReferences"}
}

func TestWorkloadOwnership(t *testing.T) {
	t.Parallel()
	cases := fixtures[[]struct {
		Name       string           `json:"name"`
		Cluster    identity         `json:"cluster"`
		Set        identity         `json:"set"`
		Pod        identity         `json:"pod"`
		SetOwners  []ownerReference `json:"setOwners"`
		PodOwners  []ownerReference `json:"podOwners"`
		Annotation string           `json:"annotation"`
		SetState   string           `json:"setState"`
		PodState   string           `json:"podState"`
	}](t, "ownership")
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			set := ownerAssociation(tc.Set, tc.Cluster, tc.SetOwners, tc.Annotation)
			equal(t, set.State, tc.SetState)
			pod := association{State: "unconfirmed"}
			if set.State == "confirmed" {
				pod = ownerAssociation(tc.Pod, tc.Set, tc.PodOwners, "")
			}
			equal(t, pod.State, tc.PodState)
		})
	}
}
