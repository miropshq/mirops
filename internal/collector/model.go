package collector

type ResourceRef struct {
	Namespace  string
	Name       string
	APIVersion string
	Kind       string
}

type ClusterSnapshot struct {
	ClusterVersion string
	NamespaceCount int
	Resources      []ResourceRef
}