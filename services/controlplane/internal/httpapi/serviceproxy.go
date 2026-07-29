package httpapi

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/zoncaesaradmin/platformkit/ctxutil"

	"appliance-code/services/controlplane/internal/appliance"
	"appliance-code/services/controlplane/internal/authz"
	"appliance-code/services/controlplane/internal/forwardauth"
	"appliance-code/services/controlplane/internal/logging"
	"appliance-code/services/controlplane/internal/serviceregistry"
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
				pattern:    rt.Method + " " + rt.ExternalPath,
				build: func(deps Deps, w wrappers) (http.Handler, error) {
					handler, err := newServiceProxyHandler(deps.Logger, reg, rt)
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

func newServiceProxyHandler(logger logging.Logger, registration ServiceProxyRegistration, route ServiceProxyRoute) (http.Handler, error) {
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
		r.Header.Del(forwardauth.HeaderAuthMethod)
		r.Header.Del(forwardauth.HeaderScopes)
		r.Header.Del(forwardauth.HeaderRoles)
		r.Header.Set(forwardauth.HeaderUserID, principal.UserID)
		r.Header.Set(forwardauth.HeaderUsername, principal.Username)
		r.Header.Set(forwardauth.HeaderAuthMethod, principal.AuthMethod)
		r.Header.Set(forwardauth.HeaderScopes, strings.Join(sortedPermissions(principal.Permissions), ","))
		r.Header.Set(forwardauth.HeaderRoles, strings.Join(sortedStrings(principal.RoleNames), ","))
		if requestID := requestIDFromRequest(r); requestID != "" {
			r.Header.Set(requestIDHeader, requestID)
		}
		if traceID, ok := ctxutil.GetTraceID(r.Context()); ok && traceID != "" {
			r.Header.Set(ctxutil.TraceIDHeader, traceID)
		}
		proxy.ServeHTTP(w, r)
	}), nil
}
