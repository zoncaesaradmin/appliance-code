package httpapi

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/zoncaesaradmin/platformkit/ctxutil"

	"appliance-code/services/controlplane/internal/appliance"
	"appliance-code/services/controlplane/internal/audit"
	"appliance-code/services/controlplane/internal/authn"
	"appliance-code/services/controlplane/internal/authz"
	"appliance-code/services/controlplane/internal/forwardauth"
	"appliance-code/services/controlplane/internal/logging"
	"appliance-code/services/controlplane/internal/serviceregistry"
	"appliance-code/services/controlplane/internal/storage"
)

type ServiceProxyRoute struct {
	Method       string
	ExternalPath string
	UpstreamPath string
	Permission   string
}

type ServiceProxyRegistration struct {
	Name       string
	Capability appliance.Capability
	BaseURL    string
	Routes     []ServiceProxyRoute
}

func RegistrationsFromRegistry(registry serviceregistry.Registry) []ServiceProxyRegistration {
	registrations := make([]ServiceProxyRegistration, 0, len(registry.Services))
	for _, service := range registry.Services {
		registration := ServiceProxyRegistration{
			Name:       strings.TrimSpace(service.Name),
			Capability: service.Capability,
			BaseURL:    strings.TrimSpace(service.BaseURL),
			Routes:     make([]ServiceProxyRoute, 0, len(service.Routes)),
		}
		for _, route := range service.Routes {
			registration.Routes = append(registration.Routes, ServiceProxyRoute{
				Method:       strings.ToUpper(strings.TrimSpace(route.Method)),
				ExternalPath: strings.TrimSpace(route.ExternalPath),
				UpstreamPath: strings.TrimSpace(route.UpstreamPath),
				Permission:   strings.TrimSpace(route.Permission),
			})
		}
		registrations = append(registrations, registration)
	}
	return registrations
}

func proxiedServiceRoutes(registrations []ServiceProxyRegistration) []publicRoute {
	var routes []publicRoute
	for _, registration := range registrations {
		reg := registration
		for _, route := range reg.Routes {
			rt := route
			routes = append(routes, publicRoute{
				capability: reg.Capability,
				moduleName: reg.Name,
				pattern:    rt.Method + " " + rt.ExternalPath,
				build: func(deps Deps, w wrappers) (http.Handler, error) {
					handler, err := newServiceProxyHandler(deps.Logger, deps.Audit, reg, rt)
					if err != nil {
						return nil, err
					}
					return w.authenticatedOnly(func(wr http.ResponseWriter, r *http.Request) {
						handler.ServeHTTP(wr, r)
					}), nil
				},
			})
		}
	}
	return routes
}

func newServiceProxyHandler(logger logging.Logger, recorder *audit.Recorder, registration ServiceProxyRegistration, route ServiceProxyRoute) (http.Handler, error) {
	target, err := url.Parse(strings.TrimSpace(registration.BaseURL))
	if err != nil {
		return nil, fmt.Errorf("parse proxied service %q base URL: %w", registration.Name, err)
	}
	if target.Scheme == "" || target.Host == "" {
		return nil, fmt.Errorf("proxied service %q base URL must be absolute", registration.Name)
	}

	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			pr.Out.URL.Path = route.UpstreamPath
			pr.Out.URL.RawPath = route.UpstreamPath
			pr.Out.Host = target.Host
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			logger.WithContext(r.Context()).Warnw("proxied service call failed",
				"service", registration.Name,
				"method", route.Method,
				"path", route.ExternalPath,
				"error", err,
			)
			WriteProblem(w, r, http.StatusBadGateway, "upstream_unavailable", "The upstream service is unavailable", "")
		},
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, ok := PrincipalFromContext(r.Context())
		if !ok {
			WriteProblem(w, r, http.StatusUnauthorized, "unauthorized", "Authentication required", "")
			return
		}
		if !authz.HasPermission(principal.Permissions, route.Permission) {
			WriteProblem(w, r, http.StatusForbidden, "forbidden", "Insufficient permissions", "")
			return
		}

		r.Header.Del(forwardauth.HeaderUserID)
		r.Header.Del(forwardauth.HeaderUsername)
		r.Header.Del(forwardauth.HeaderAuthDomain)
		r.Header.Del(forwardauth.HeaderAuthMethod)
		r.Header.Del(forwardauth.HeaderScopes)
		r.Header.Del(forwardauth.HeaderRoles)
		domain := principal.Domain
		if domain == "" {
			domain = authn.AuthDomainLocal
		}
		r.Header.Set(forwardauth.HeaderUserID, principal.UserID)
		r.Header.Set(forwardauth.HeaderUsername, principal.Username)
		r.Header.Set(forwardauth.HeaderAuthDomain, domain)
		r.Header.Set(forwardauth.HeaderAuthMethod, principal.AuthMethod)
		r.Header.Set(forwardauth.HeaderScopes, strings.Join(sortedPermissions(principal.Permissions), ","))
		r.Header.Set(forwardauth.HeaderRoles, strings.Join(sortedStrings(principal.RoleNames), ","))
		if requestID := requestIDFromRequest(r); requestID != "" {
			r.Header.Set(requestIDHeader, requestID)
		}
		if traceID, ok := ctxutil.GetTraceID(r.Context()); ok && traceID != "" {
			r.Header.Set(ctxutil.TraceIDHeader, traceID)
		}

		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		proxy.ServeHTTP(rec, r)

		if recorder != nil && isMutatingProxyMethod(route.Method) && rec.status >= 200 && rec.status < 300 {
			action, targetType := proxiedMutationAudit(route.ExternalPath)
			_ = recorder.Record(r.Context(), principal.Actor(requestIDFromRequest(r), r.RemoteAddr), audit.Event{
				Action: action, TargetType: targetType, TargetID: route.ExternalPath,
				Outcome: storage.AuditOutcomeSuccess,
				Details: map[string]any{"method": route.Method, "service": registration.Name, "status": rec.status},
			})
		}
	}), nil
}

func isMutatingProxyMethod(method string) bool {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func proxiedMutationAudit(externalPath string) (action, targetType string) {
	switch {
	case strings.HasSuffix(externalPath, "/host/wifi"):
		return "host.wifi.update", "host_wifi"
	case strings.HasSuffix(externalPath, "/host/wifi-ap"):
		return "host.wifi_ap.update", "host_wifi_ap"
	case strings.HasSuffix(externalPath, "/host/mdns"):
		return "host.mdns.update", "host_mdns"
	default:
		return "host.proxy.mutate", "host_proxy"
	}
}
