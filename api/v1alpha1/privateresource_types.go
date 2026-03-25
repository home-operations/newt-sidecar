package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PrivateResourceSpec defines the desired state of a Pangolin private-resource.
// Each CR produces one entry in the blueprint's private-resources block.
// The site field is intentionally absent — it is injected from the sidecar's --site-id flag.
type PrivateResourceSpec struct {
	// Name is the display name of the private resource in Pangolin.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=255
	Name string `json:"name"`

	// Mode controls whether this resource tunnels to a single host or a CIDR range.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=host;cidr
	Mode string `json:"mode"`

	// Destination is the IP address, hostname, or CIDR range to tunnel to.
	// In cidr mode this must be a valid CIDR (e.g. 10.42.0.0/16).
	// In host mode this can be an IP address or a hostname (alias required for hostnames).
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Destination string `json:"destination"`

	// TcpPorts is the set of TCP ports to expose. Defaults to "*" (all ports).
	// +kubebuilder:default="*"
	// +optional
	TcpPorts string `json:"tcpPorts,omitempty"`

	// UdpPorts is the set of UDP ports to expose. Defaults to "*" (all ports).
	// +kubebuilder:default="*"
	// +optional
	UdpPorts string `json:"udpPorts,omitempty"`

	// DisableIcmp disables ICMP (ping) tunnelling for this resource.
	// +kubebuilder:default=false
	// +optional
	DisableIcmp bool `json:"disableIcmp,omitempty"`

	// Alias is a fully-qualified domain name alias for the resource.
	// Required when mode is "host" and destination is a hostname (not an IP).
	// +optional
	Alias string `json:"alias,omitempty"`

	// Roles restricts access to OLM clients that have one of these Pangolin roles.
	// Must not include "Admin".
	// +optional
	Roles []string `json:"roles,omitempty"`

	// Users restricts access to these Pangolin user email addresses.
	// +optional
	Users []string `json:"users,omitempty"`

	// Machines restricts access to these Pangolin machine client IDs.
	// +optional
	Machines []string `json:"machines,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,shortName=privr
// +kubebuilder:printcolumn:name="Mode",type="string",JSONPath=".spec.mode"
// +kubebuilder:printcolumn:name="Destination",type="string",JSONPath=".spec.destination"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// PrivateResource is the Schema for Pangolin private-resources.
// Each instance produces one entry in the blueprint's private-resources block,
// enabling OLM VPN clients to reach cluster-internal networks or hosts.
type PrivateResource struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec PrivateResourceSpec `json:"spec,omitempty"`
}

// +kubebuilder:object:root=true

// PrivateResourceList contains a list of PrivateResource
type PrivateResourceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PrivateResource `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PrivateResource{}, &PrivateResourceList{})
}
