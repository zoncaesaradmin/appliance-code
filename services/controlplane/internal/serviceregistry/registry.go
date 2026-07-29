package serviceregistry

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"appliance-code/services/controlplane/internal/appliance"
)

type Registry struct {
	Services []Service `json:"services"`
}

type Service struct {
	Name       string               `json:"name"`
	Capability appliance.Capability `json:"capability"`
	BaseURL    string               `json:"baseURL"`
	Routes     []Route              `json:"routes"`
}

type Route struct {
	Method       string `json:"method"`
	ExternalPath string `json:"externalPath"`
	UpstreamPath string `json:"upstreamPath"`
	Permission   string `json:"permission"`
}

func RegistryFromModules(modules []appliance.ModuleDescriptor) Registry {
	registry := Registry{Services: make([]Service, 0, len(modules))}
	for _, module := range modules {
		if strings.TrimSpace(module.BaseURL) == "" || len(module.Routes) == 0 {
			continue
		}
		service := Service{
			Name:       strings.TrimSpace(module.Name),
			Capability: module.PrimaryCapability(),
			BaseURL:    strings.TrimSpace(module.BaseURL),
			Routes:     make([]Route, 0, len(module.Routes)),
		}
		for _, route := range module.Routes {
			service.Routes = append(service.Routes, Route{
				Method:       strings.ToUpper(strings.TrimSpace(route.Method)),
				ExternalPath: strings.TrimSpace(route.ExternalPath),
				UpstreamPath: strings.TrimSpace(route.UpstreamPath),
				Permission:   strings.TrimSpace(route.Permission),
			})
		}
		registry.Services = append(registry.Services, service)
	}
	return registry
}

func (r Registry) Validate(enabled appliance.Set) error {
	seen := make(map[string]string)
	var errs []string
	for idx, service := range r.Services {
		label := fmt.Sprintf("serviceRegistry.services[%d]", idx)
		if strings.TrimSpace(service.Name) == "" {
			errs = append(errs, label+".name must not be empty")
		}
		if strings.TrimSpace(string(service.Capability)) == "" {
			errs = append(errs, label+".capability must not be empty")
		} else if !enabled.Enabled(service.Capability) {
			errs = append(errs, fmt.Sprintf("%s.capability %q is not enabled by applianceProfile", label, service.Capability))
		}
		if baseURL := strings.TrimSpace(service.BaseURL); baseURL == "" {
			errs = append(errs, label+".baseURL must not be empty")
		} else if u, err := url.Parse(baseURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			errs = append(errs, label+".baseURL must be an absolute http(s) URL")
		}
		if len(service.Routes) == 0 {
			errs = append(errs, label+".routes must not be empty")
			continue
		}
		for routeIdx, route := range service.Routes {
			routeLabel := fmt.Sprintf("%s.routes[%d]", label, routeIdx)
			method := strings.ToUpper(strings.TrimSpace(route.Method))
			if method == "" {
				errs = append(errs, routeLabel+".method must not be empty")
			} else if !validMethod(method) {
				errs = append(errs, fmt.Sprintf("%s.method %q is not supported", routeLabel, route.Method))
			}
			if !strings.HasPrefix(strings.TrimSpace(route.ExternalPath), "/") {
				errs = append(errs, routeLabel+".externalPath must start with /")
			}
			if !strings.HasPrefix(strings.TrimSpace(route.UpstreamPath), "/") {
				errs = append(errs, routeLabel+".upstreamPath must start with /")
			}
			if strings.TrimSpace(route.Permission) == "" {
				errs = append(errs, routeLabel+".permission must not be empty")
			}
			if method != "" && strings.HasPrefix(strings.TrimSpace(route.ExternalPath), "/") {
				key := method + " " + strings.TrimSpace(route.ExternalPath)
				if existing, ok := seen[key]; ok {
					errs = append(errs, fmt.Sprintf("%s duplicates route %s already owned by %s", routeLabel, key, existing))
				} else {
					seen[key] = strings.TrimSpace(service.Name)
				}
			}
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

func validMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodHead, http.MethodOptions:
		return true
	default:
		return false
	}
}
