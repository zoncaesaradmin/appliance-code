package appliance

import "fmt"

type ModuleCatalogLoader interface {
	LoadModuleCatalog() ([]ModuleDescriptor, error)
}

type StaticModuleCatalogLoader struct {
	Modules []ModuleDescriptor
}

func (l StaticModuleCatalogLoader) LoadModuleCatalog() ([]ModuleDescriptor, error) {
	if l.Modules == nil {
		return nil, fmt.Errorf("module catalog loader has no catalog")
	}
	return append([]ModuleDescriptor(nil), l.Modules...), nil
}
