package content

import (
	v1 "metapus/internal/infrastructure/http/v1"
	"metapus/internal/metadata"
)

// BuildReportRegistry uses the same factories and policy declarations as HTTP.
func BuildReportRegistry() *metadata.Registry {
	factories := v1.NewFactoryRegistry()
	RegisterDefaults(factories)
	return v1.BuildReadRegistry(factories)
}
