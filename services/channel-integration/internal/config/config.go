// Package config loads the channel integration runtime configuration.
package config

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"regexp"
	"strings"
)

const maxConfigBytes = 64 << 10

var graphVersionPattern = regexp.MustCompile(`^v[0-9]+(?:\.[0-9]+)?$`)

type Config struct {
	ListenAddress     string
	VerifyToken       string
	AppSecret         string
	GraphAccessToken  string
	PhoneNumberID     string
	GraphAPIVersion   string
	IdentityURL       string
	IdentityKey       string
	IdentityTenantID  string
	IdentityChannelID string
}

func Load(get func(string) string) (Config, error) {
	c := Config{ListenAddress: strings.TrimSpace(get("CAPP_LISTEN_ADDR"))}
	if c.ListenAddress == "" {
		c.ListenAddress = ":8080"
	}
	if _, _, err := net.SplitHostPort(c.ListenAddress); err != nil {
		return Config{}, errors.New("CAPP_LISTEN_ADDR must be a host:port address")
	}
	runtime, err := loadRuntimeConfig(strings.TrimSpace(get("CAPP_WHATSAPP_CONFIG_FILE")))
	if err != nil {
		return Config{}, err
	}
	c.VerifyToken, c.AppSecret, c.GraphAccessToken = strings.TrimSpace(runtime.VerifyToken), strings.TrimSpace(runtime.AppSecret), strings.TrimSpace(runtime.GraphAccessToken)
	c.PhoneNumberID, c.GraphAPIVersion = strings.TrimSpace(runtime.PhoneNumberID), strings.TrimSpace(runtime.GraphAPIVersion)
	c.IdentityURL = strings.TrimSpace(get("CAPP_APPLICATION_IDENTITY_URL"))
	c.IdentityKey = strings.TrimSpace(get("CAPP_APPLICATION_IDENTITY_KEY"))
	c.IdentityTenantID, c.IdentityChannelID = strings.TrimSpace(runtime.TenantID), strings.TrimSpace(runtime.ChannelID)
	if c.IdentityTenantID == "" {
		c.IdentityTenantID = "zonited-solutions"
	}
	if c.IdentityChannelID == "" {
		c.IdentityChannelID = "customer-whatsapp"
	}
	for _, required := range []struct{ name, value string }{{"verify_token", c.VerifyToken}, {"app_secret", c.AppSecret}, {"graph_access_token", c.GraphAccessToken}, {"phone_number_id", c.PhoneNumberID}, {"graph_api_version", c.GraphAPIVersion}} {
		if required.value == "" {
			return Config{}, fmt.Errorf("%s is required", required.name)
		}
	}
	if !graphVersionPattern.MatchString(c.GraphAPIVersion) {
		return Config{}, errors.New("graph_api_version must look like v21.0")
	}
	if c.IdentityURL != "" {
		if c.IdentityKey == "" {
			return Config{}, errors.New("CAPP_APPLICATION_IDENTITY_KEY is required when identity resolution is enabled")
		}
		decoded, err := base64.StdEncoding.DecodeString(c.IdentityKey)
		if err != nil || len(decoded) < 32 {
			return Config{}, errors.New("CAPP_APPLICATION_IDENTITY_KEY must be base64-encoded and at least 32 bytes")
		}
	}
	for _, char := range c.PhoneNumberID {
		if char < '0' || char > '9' {
			return Config{}, errors.New("phone_number_id must contain only digits")
		}
	}
	return c, nil
}

type runtimeConfig struct {
	VerifyToken      string `json:"verify_token"`
	AppSecret        string `json:"app_secret"`
	GraphAccessToken string `json:"graph_access_token"`
	PhoneNumberID    string `json:"phone_number_id"`
	GraphAPIVersion  string `json:"graph_api_version"`
	TenantID         string `json:"tenant_id"`
	ChannelID        string `json:"channel_id"`
}

func loadRuntimeConfig(path string) (runtimeConfig, error) {
	if path == "" {
		return runtimeConfig{}, errors.New("CAPP_WHATSAPP_CONFIG_FILE is required")
	}
	file, err := os.Open(path)
	if err != nil {
		return runtimeConfig{}, errors.New("channel integration runtime configuration is unavailable")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxConfigBytes {
		return runtimeConfig{}, errors.New("channel integration runtime configuration is invalid")
	}
	decoder := json.NewDecoder(io.LimitReader(file, maxConfigBytes+1))
	decoder.DisallowUnknownFields()
	var config runtimeConfig
	if err := decoder.Decode(&config); err != nil {
		return runtimeConfig{}, errors.New("channel integration runtime configuration is invalid")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return runtimeConfig{}, errors.New("channel integration runtime configuration is invalid")
	}
	return config, nil
}
