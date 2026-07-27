package profilerprobe

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	pyroscope "github.com/grafana/pyroscope-go"
)

func TestLoadWithLookupDisabledByDefault(t *testing.T) {
	config, err := LoadWithLookup(mapLookup(nil))
	if err != nil {
		t.Fatalf("LoadWithLookup() error = %v", err)
	}
	if config.Enabled {
		t.Fatal("LoadWithLookup() enabled profiling by default")
	}
}

func TestLoadWithLookupRejectsNilLookup(t *testing.T) {
	_, err := LoadWithLookup(nil)
	if err == nil || !strings.Contains(err.Error(), "lookup is nil") {
		t.Fatalf("LoadWithLookup(nil) error = %v, want nil lookup error", err)
	}
}

func TestLoadWithLookupIgnoresDisabledSettings(t *testing.T) {
	config, err := loadWith(mapLookup(map[string]string{
		EnvEnabled:               "false",
		EnvAdhocServerAddress:    "http://untrusted.invalid",
		EnvAllowInsecureHTTP:     "not-a-bool",
		EnvTags:                  "not-a-tag",
		EnvBasicAuthPasswordFile: "missing-file",
	}), func(string) ([]byte, error) {
		t.Fatal("disabled configuration read a password file")
		return nil, nil
	})
	if err != nil || config.Enabled {
		t.Fatalf("loadWith() = (%#v, %v), want disabled config", config, err)
	}
}

func TestLoadWithLookupDefaults(t *testing.T) {
	config, err := LoadWithLookup(mapLookup(map[string]string{
		EnvEnabled:         "true",
		EnvServerAddress:   " https://pyroscope.internal:4040/ ",
		EnvApplicationName: " orders-api ",
	}))
	if err != nil {
		t.Fatalf("LoadWithLookup() error = %v", err)
	}

	want := Config{
		Enabled:              true,
		ServerAddress:        "https://pyroscope.internal:4040/",
		ApplicationName:      "orders-api",
		DisableGCRuns:        true,
		UploadRate:           15 * time.Second,
		HTTPTimeout:          5 * time.Second,
		ProfileCPU:           true,
		ProfileAllocSpace:    true,
		ProfileInuseSpace:    true,
		ProfileGoroutines:    true,
		MutexProfileFraction: 5,
		BlockProfileRate:     5,
	}
	if !reflect.DeepEqual(config, want) {
		t.Fatalf("LoadWithLookup() = %#v, want %#v", config, want)
	}
}

func TestLoadWithLookupBuildsFullConfig(t *testing.T) {
	config, err := LoadWithLookup(mapLookup(map[string]string{
		EnvEnabled:              " true ",
		EnvServerAddress:        "http://pyroscope.monitoring.svc:4040/base",
		EnvAllowInsecureHTTP:    "true",
		EnvApplicationName:      "checkout/api_v2",
		EnvTags:                 " environment=staging, region=ap-southeast-1 ",
		EnvTenantID:             "tenant-a",
		EnvBasicAuthUser:        "uploader",
		EnvBasicAuthPassword:    " password with spaces ",
		EnvLogging:              "true",
		EnvDisableGCRuns:        "false",
		EnvUploadRate:           "30s",
		EnvHTTPTimeout:          "7s",
		EnvProfileCPU:           "false",
		EnvProfileAllocObjects:  "true",
		EnvProfileAllocSpace:    "false",
		EnvProfileInuseObjects:  "true",
		EnvProfileInuseSpace:    "false",
		EnvProfileGoroutines:    "false",
		EnvProfileMutex:         "true",
		EnvProfileBlock:         "true",
		EnvMutexProfileFraction: "7",
		EnvBlockProfileRate:     "11",
	}))
	if err != nil {
		t.Fatalf("LoadWithLookup() error = %v", err)
	}

	want := Config{
		Enabled:              true,
		ServerAddress:        "http://pyroscope.monitoring.svc:4040/base",
		AllowInsecureHTTP:    true,
		ApplicationName:      "checkout/api_v2",
		Tags:                 map[string]string{"environment": "staging", "region": "ap-southeast-1"},
		TenantID:             "tenant-a",
		BasicAuthUser:        "uploader",
		BasicAuthPassword:    " password with spaces ",
		Logging:              true,
		DisableGCRuns:        false,
		UploadRate:           30 * time.Second,
		HTTPTimeout:          7 * time.Second,
		ProfileCPU:           false,
		ProfileAllocObjects:  true,
		ProfileAllocSpace:    false,
		ProfileInuseObjects:  true,
		ProfileInuseSpace:    false,
		ProfileGoroutines:    false,
		ProfileMutex:         true,
		ProfileBlock:         true,
		MutexProfileFraction: 7,
		BlockProfileRate:     11,
	}
	if !reflect.DeepEqual(config, want) {
		t.Fatalf("LoadWithLookup() = %#v, want %#v", config, want)
	}
}

func TestLoadReadsProcessEnvironment(t *testing.T) {
	clearProfilerEnvironment(t)
	t.Setenv(EnvEnabled, "true")
	t.Setenv(EnvServerAddress, "https://profiles.example.test")
	t.Setenv(EnvApplicationName, "inventory")

	config, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !config.Enabled || config.ApplicationName != "inventory" || config.ServerAddress != "https://profiles.example.test" {
		t.Fatalf("Load() = %#v, want enabled inventory config", config)
	}
}

func TestLoadWithLookupRejectsUpstreamAddressOverride(t *testing.T) {
	for _, value := range []string{"https://different.example.test", ""} {
		_, err := LoadWithLookup(mapLookup(map[string]string{
			EnvEnabled:            "true",
			EnvAdhocServerAddress: value,
		}))
		if err == nil || !strings.Contains(err.Error(), EnvAdhocServerAddress) {
			t.Fatalf("LoadWithLookup() error = %v, want override rejection", err)
		}
	}
}

func TestLoadWithLookupRejectsInvalidScalarValues(t *testing.T) {
	booleanKeys := []string{
		EnvEnabled,
		EnvAllowInsecureHTTP,
		EnvLogging,
		EnvDisableGCRuns,
		EnvProfileCPU,
		EnvProfileAllocObjects,
		EnvProfileAllocSpace,
		EnvProfileInuseObjects,
		EnvProfileInuseSpace,
		EnvProfileGoroutines,
		EnvProfileMutex,
		EnvProfileBlock,
	}
	for _, key := range booleanKeys {
		t.Run(key, func(t *testing.T) {
			values := validEnvironment()
			values[key] = "sometimes"
			_, err := LoadWithLookup(mapLookup(values))
			if err == nil || !strings.Contains(err.Error(), key) {
				t.Fatalf("LoadWithLookup() error = %v, want error mentioning %s", err, key)
			}
		})
	}

	tests := []struct {
		key   string
		value string
	}{
		{EnvUploadRate, "soon"},
		{EnvHTTPTimeout, "eventually"},
		{EnvMutexProfileFraction, "many"},
		{EnvBlockProfileRate, "often"},
	}
	for _, test := range tests {
		t.Run(test.key, func(t *testing.T) {
			values := validEnvironment()
			values[test.key] = test.value
			_, err := LoadWithLookup(mapLookup(values))
			if err == nil || !strings.Contains(err.Error(), test.key) {
				t.Fatalf("LoadWithLookup() error = %v, want error mentioning %s", err, test.key)
			}
		})
	}
}

func TestTagsFromEnv(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    map[string]string
		wantErr string
	}{
		{name: "absent"},
		{name: "valid", raw: "env=prod,service_version=v1", want: map[string]string{"env": "prod", "service_version": "v1"}},
		{name: "missing equals", raw: "env", wantErr: "key=value"},
		{name: "too many equals", raw: "env=prod=blue", wantErr: "key=value"},
		{name: "empty entry", raw: "env=prod,", wantErr: "key=value"},
		{name: "empty key", raw: "=prod", wantErr: "non-empty"},
		{name: "empty value", raw: "env=", wantErr: "non-empty"},
		{name: "duplicate", raw: "env=prod,env=stage", wantErr: "duplicate"},
		{name: "invalid key", raw: "bad-key=value", wantErr: "invalid"},
		{name: "dotted key", raw: "service.name=value", wantErr: "invalid"},
		{name: "reserved key", raw: "__name__=value", wantErr: "reserved"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := tagsFromEnv(mapLookup(map[string]string{EnvTags: test.raw}))
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("tagsFromEnv() error = %v, want %q", err, test.wantErr)
				}
				return
			}
			if err != nil || !reflect.DeepEqual(got, test.want) {
				t.Fatalf("tagsFromEnv() = (%#v, %v), want (%#v, nil)", got, err, test.want)
			}
		})
	}
}

func TestBasicAuthPasswordFromEnv(t *testing.T) {
	secret := "never-print-this-secret"
	tests := []struct {
		name      string
		values    map[string]string
		contents  []byte
		readErr   error
		nilReader bool
		want      string
		wantErr   string
	}{
		{name: "absent"},
		{name: "environment", values: map[string]string{EnvBasicAuthPassword: secret}, want: secret},
		{name: "file LF", values: map[string]string{EnvBasicAuthPasswordFile: "/secret/password"}, contents: []byte(secret + "\n"), want: secret},
		{name: "file CRLF", values: map[string]string{EnvBasicAuthPasswordFile: "/secret/password"}, contents: []byte(secret + "\r\n"), want: secret},
		{name: "mutually exclusive", values: map[string]string{EnvBasicAuthPassword: secret, EnvBasicAuthPasswordFile: "/secret/password"}, wantErr: "mutually exclusive"},
		{name: "read failure", values: map[string]string{EnvBasicAuthPasswordFile: "/secret/password"}, readErr: errors.New("permission denied"), wantErr: EnvBasicAuthPasswordFile},
		{name: "nil reader", values: map[string]string{EnvBasicAuthPasswordFile: "/secret/password"}, nilReader: true, wantErr: "reader is nil"},
		{name: "empty file", values: map[string]string{EnvBasicAuthPasswordFile: "/secret/password"}, wantErr: "is empty"},
		{name: "oversize", values: map[string]string{EnvBasicAuthPasswordFile: "/secret/password"}, contents: make([]byte, maximumSecretFileBytes+1), wantErr: "exceeds"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var reader readFileFunc
			if !test.nilReader {
				reader = func(path string) ([]byte, error) {
					if path != "/secret/password" {
						t.Fatalf("reader path = %q", path)
					}
					return test.contents, test.readErr
				}
			}
			got, err := basicAuthPasswordFromEnv(mapLookup(test.values), reader)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("basicAuthPasswordFromEnv() error = %v, want %q", err, test.wantErr)
				}
				if strings.Contains(err.Error(), secret) {
					t.Fatalf("error leaked password: %v", err)
				}
				return
			}
			if err != nil || got != test.want {
				t.Fatalf("basicAuthPasswordFromEnv() = (%q, %v), want (%q, nil)", got, err, test.want)
			}
		})
	}
}

func TestReadSecretFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "password")
	if err := os.WriteFile(path, []byte("secret\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	contents, err := readSecretFile(path)
	if err != nil || string(contents) != "secret\n" {
		t.Fatalf("readSecretFile() = (%q, %v)", contents, err)
	}
	if _, err := readSecretFile(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("readSecretFile() accepted a missing file")
	}
}

func TestLoadWithPasswordFile(t *testing.T) {
	values := validEnvironment()
	values[EnvBasicAuthUser] = "uploader"
	values[EnvBasicAuthPasswordFile] = "/secret/password"

	config, err := loadWith(mapLookup(values), func(path string) ([]byte, error) {
		if path != "/secret/password" {
			t.Fatalf("password path = %q", path)
		}
		return []byte("file-secret\n"), nil
	})
	if err != nil {
		t.Fatalf("loadWith() error = %v", err)
	}
	if config.BasicAuthPassword != "file-secret" {
		t.Fatalf("password = %q, want file-secret", config.BasicAuthPassword)
	}
}

func TestConfigValidate(t *testing.T) {
	tooManyTags := make(map[string]string, maximumTags+1)
	for i := 0; i <= maximumTags; i++ {
		tooManyTags[fmt.Sprintf("tag%d", i)] = "value"
	}

	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{name: "valid"},
		{name: "zero upload uses SDK default", mutate: func(c *Config) { c.UploadRate = 0 }},
		{name: "plain HTTP explicitly allowed", mutate: func(c *Config) { c.ServerAddress = "http://pyroscope:4040"; c.AllowInsecureHTTP = true }},
		{name: "missing server", mutate: func(c *Config) { c.ServerAddress = "" }, wantErr: EnvServerAddress},
		{name: "relative server", mutate: func(c *Config) { c.ServerAddress = "/pyroscope" }, wantErr: "absolute"},
		{name: "unsupported scheme", mutate: func(c *Config) { c.ServerAddress = "ftp://pyroscope.example.test" }, wantErr: "http or https"},
		{name: "plain HTTP denied", mutate: func(c *Config) { c.ServerAddress = "http://pyroscope:4040" }, wantErr: EnvAllowInsecureHTTP},
		{name: "URL credentials", mutate: func(c *Config) { c.ServerAddress = "https://user:secret@pyroscope.example.test" }, wantErr: "must not contain credentials"},
		{name: "URL query", mutate: func(c *Config) { c.ServerAddress = "https://pyroscope.example.test?tenant=a" }, wantErr: "query or fragment"},
		{name: "URL fragment", mutate: func(c *Config) { c.ServerAddress = "https://pyroscope.example.test/#fragment" }, wantErr: "query or fragment"},
		{name: "missing application", mutate: func(c *Config) { c.ApplicationName = "" }, wantErr: EnvApplicationName},
		{name: "invalid application", mutate: func(c *Config) { c.ApplicationName = "orders api" }, wantErr: "must be at most"},
		{name: "too long application", mutate: func(c *Config) { c.ApplicationName = strings.Repeat("a", maximumApplicationNameBytes+1) }, wantErr: "must be at most"},
		{name: "reserved tag", mutate: func(c *Config) { c.Tags = map[string]string{"__name__": "bad"} }, wantErr: "reserved"},
		{name: "invalid tag", mutate: func(c *Config) { c.Tags = map[string]string{"bad-key": "value"} }, wantErr: "invalid"},
		{name: "long tag key", mutate: func(c *Config) { c.Tags = map[string]string{strings.Repeat("a", maximumTagNameBytes+1): "value"} }, wantErr: "invalid"},
		{name: "empty tag value", mutate: func(c *Config) { c.Tags = map[string]string{"environment": "  "} }, wantErr: "empty value"},
		{name: "long tag value", mutate: func(c *Config) {
			c.Tags = map[string]string{"environment": strings.Repeat("a", maximumTagValueBytes+1)}
		}, wantErr: "at most"},
		{name: "control in tag value", mutate: func(c *Config) { c.Tags = map[string]string{"environment": "prod\nother"} }, wantErr: "must be at most"},
		{name: "comma in tag value", mutate: func(c *Config) { c.Tags = map[string]string{"environment": "prod,team=other"} }, wantErr: "must be at most"},
		{name: "opening brace in tag value", mutate: func(c *Config) { c.Tags = map[string]string{"environment": "prod{other"} }, wantErr: "must be at most"},
		{name: "closing brace in tag value", mutate: func(c *Config) { c.Tags = map[string]string{"environment": "prod}"} }, wantErr: "must be at most"},
		{name: "equals in tag value", mutate: func(c *Config) { c.Tags = map[string]string{"environment": "prod=other"} }, wantErr: "must be at most"},
		{name: "quote in tag value", mutate: func(c *Config) { c.Tags = map[string]string{"environment": `prod"other`} }, wantErr: "must be at most"},
		{name: "backslash in tag value", mutate: func(c *Config) { c.Tags = map[string]string{"environment": `prod\other`} }, wantErr: "must be at most"},
		{name: "space in tag value", mutate: func(c *Config) { c.Tags = map[string]string{"environment": "prod other"} }, wantErr: "must be at most"},
		{name: "too many tags", mutate: func(c *Config) { c.Tags = tooManyTags }, wantErr: "at most"},
		{name: "valid tenant", mutate: func(c *Config) { c.TenantID = "platform.team-1" }},
		{name: "invalid tenant", mutate: func(c *Config) { c.TenantID = "tenant a" }, wantErr: EnvTenantID},
		{name: "long tenant", mutate: func(c *Config) { c.TenantID = strings.Repeat("a", 129) }, wantErr: EnvTenantID},
		{name: "user without password", mutate: func(c *Config) { c.BasicAuthUser = "uploader" }, wantErr: EnvBasicAuthPassword},
		{name: "password without user", mutate: func(c *Config) { c.BasicAuthPassword = "do-not-leak" }, wantErr: EnvBasicAuthUser},
		{name: "colon in user", mutate: func(c *Config) { c.BasicAuthUser = "bad:user"; c.BasicAuthPassword = "do-not-leak" }, wantErr: EnvBasicAuthUser},
		{name: "long user", mutate: func(c *Config) {
			c.BasicAuthUser = strings.Repeat("a", maximumBasicAuthUserBytes+1)
			c.BasicAuthPassword = "do-not-leak"
		}, wantErr: "exceeds"},
		{name: "newline in password", mutate: func(c *Config) { c.BasicAuthUser = "user"; c.BasicAuthPassword = "do-not-leak\n" }, wantErr: "unsupported"},
		{name: "long password", mutate: func(c *Config) {
			c.BasicAuthUser = "user"
			c.BasicAuthPassword = strings.Repeat("x", maximumBasicAuthPasswordBytes+1)
		}, wantErr: "exceeds"},
		{name: "upload too fast", mutate: func(c *Config) { c.UploadRate = minimumUploadRate - time.Nanosecond }, wantErr: EnvUploadRate},
		{name: "upload too slow", mutate: func(c *Config) { c.UploadRate = maximumUploadRate + time.Nanosecond }, wantErr: EnvUploadRate},
		{name: "zero HTTP timeout uses wrapper default", mutate: func(c *Config) { c.HTTPTimeout = 0 }},
		{name: "HTTP timeout too short", mutate: func(c *Config) { c.HTTPTimeout = minimumHTTPTimeout - time.Nanosecond }, wantErr: EnvHTTPTimeout},
		{name: "HTTP timeout too long", mutate: func(c *Config) { c.HTTPTimeout = maximumHTTPTimeout + time.Nanosecond }, wantErr: EnvHTTPTimeout},
		{name: "no profiles", mutate: disableAllProfiles, wantErr: "at least one profile"},
		{name: "bad mutex fraction", mutate: func(c *Config) { disableAllProfiles(c); c.ProfileMutex = true; c.MutexProfileFraction = 0 }, wantErr: EnvMutexProfileFraction},
		{name: "bad block rate", mutate: func(c *Config) { disableAllProfiles(c); c.ProfileBlock = true; c.BlockProfileRate = -1 }, wantErr: EnvBlockProfileRate},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := validConfig()
			if test.mutate != nil {
				test.mutate(&config)
			}
			err := config.Validate()
			if test.wantErr == "" && err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
			if test.wantErr != "" && (err == nil || !strings.Contains(err.Error(), test.wantErr)) {
				t.Fatalf("Validate() error = %v, want error containing %q", err, test.wantErr)
			}
			if err != nil && strings.Contains(err.Error(), "do-not-leak") {
				t.Fatalf("Validate() leaked password: %v", err)
			}
		})
	}

	if err := (Config{}).Validate(); err != nil {
		t.Fatalf("disabled Validate() error = %v", err)
	}
}

func TestPyroscopeConfig(t *testing.T) {
	config := validConfig()
	config.Tags = map[string]string{"environment": "staging"}
	config.TenantID = "tenant-a"
	config.BasicAuthUser = "uploader"
	config.BasicAuthPassword = "secret"
	config.Logging = true
	config.DisableGCRuns = true
	config.ProfileAllocObjects = true
	config.ProfileAllocSpace = true
	config.ProfileInuseObjects = true
	config.ProfileInuseSpace = true
	config.ProfileGoroutines = true
	config.ProfileMutex = true
	config.ProfileBlock = true

	got := config.pyroscopeConfig()
	wantTypes := []pyroscope.ProfileType{
		pyroscope.ProfileCPU,
		pyroscope.ProfileAllocObjects,
		pyroscope.ProfileAllocSpace,
		pyroscope.ProfileInuseObjects,
		pyroscope.ProfileInuseSpace,
		pyroscope.ProfileGoroutines,
		pyroscope.ProfileMutexCount,
		pyroscope.ProfileMutexDuration,
		pyroscope.ProfileBlockCount,
		pyroscope.ProfileBlockDuration,
	}
	if got.ApplicationName != config.ApplicationName || got.ServerAddress != config.ServerAddress ||
		got.TenantID != config.TenantID || got.BasicAuthUser != config.BasicAuthUser ||
		got.BasicAuthPassword != config.BasicAuthPassword || got.UploadRate != config.UploadRate ||
		!got.DisableGCRuns || got.Logger != pyroscope.StandardLogger ||
		!reflect.DeepEqual(got.ProfileTypes, wantTypes) || !reflect.DeepEqual(got.Tags, config.Tags) {
		t.Fatalf("pyroscopeConfig() = %#v, want all fields propagated", got)
	}
	httpClient, ok := got.HTTPClient.(*http.Client)
	if !ok || httpClient.Timeout != config.HTTPTimeout {
		t.Fatalf("pyroscopeConfig() HTTP client = %#v, want timeout %s", got.HTTPClient, config.HTTPTimeout)
	}
	if err := httpClient.CheckRedirect(&http.Request{}, nil); !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatalf("HTTP client redirect error = %v, want http.ErrUseLastResponse", err)
	}
	got.Tags["environment"] = "changed"
	if config.Tags["environment"] != "staging" {
		t.Fatal("pyroscopeConfig() did not clone Tags")
	}

	config.Logging = false
	config.Tags = nil
	got = config.pyroscopeConfig()
	if got.Logger != nil || got.Tags != nil {
		t.Fatalf("pyroscopeConfig() logger/tags = (%#v, %#v), want nil", got.Logger, got.Tags)
	}
}

func TestStartWith(t *testing.T) {
	t.Run("disabled does not call starter", func(t *testing.T) {
		called := false
		started, err := startWith(Config{}, func(pyroscope.Config) (profilerHandle, error) {
			called = true
			return &fakeProfiler{}, nil
		})
		if err != nil || started || called {
			t.Fatalf("startWith() = (%t, %v), called=%t; want disabled", started, err, called)
		}
	})

	t.Run("passes private SDK config", func(t *testing.T) {
		config := validConfig()
		started, err := startWith(config, func(got pyroscope.Config) (profilerHandle, error) {
			if got.ApplicationName != config.ApplicationName || got.ServerAddress != config.ServerAddress || len(got.ProfileTypes) != 1 {
				t.Fatalf("starter config = %#v", got)
			}
			return &fakeProfiler{}, nil
		})
		if err != nil || !started {
			t.Fatalf("startWith() = (%t, %v), want success", started, err)
		}
	})

	t.Run("wraps starter error and permits retry", func(t *testing.T) {
		gate := new(startGate)
		want := errors.New("invalid SDK configuration")
		calls := 0
		start := func(pyroscope.Config) (profilerHandle, error) {
			calls++
			if calls == 1 {
				return nil, want
			}
			return &fakeProfiler{}, nil
		}
		settings := fakeRuntimeSettings(nil, nil)
		started, err := startWithGate(validConfig(), gate, start, settings)
		if started || !errors.Is(err, want) {
			t.Fatalf("first start = (%t, %v), want wrapped error", started, err)
		}
		started, err = startWithGate(validConfig(), gate, start, settings)
		if err != nil || !started {
			t.Fatalf("retry = (%t, %v), want success", started, err)
		}
	})

	t.Run("rejects invalid config before call", func(t *testing.T) {
		called := false
		started, err := startWith(Config{Enabled: true}, func(pyroscope.Config) (profilerHandle, error) {
			called = true
			return &fakeProfiler{}, nil
		})
		if started || err == nil || called {
			t.Fatalf("startWith() = (%t, %v), called=%t", started, err, called)
		}
	})

	t.Run("rejects nil dependencies", func(t *testing.T) {
		config := validConfig()
		if started, err := startWithGate(config, nil, validStarter(), fakeRuntimeSettings(nil, nil)); started || err == nil || !strings.Contains(err.Error(), "gate is nil") {
			t.Fatalf("nil gate = (%t, %v)", started, err)
		}
		if started, err := startWithGate(config, new(startGate), nil, fakeRuntimeSettings(nil, nil)); started || err == nil || !strings.Contains(err.Error(), "starter is nil") {
			t.Fatalf("nil starter = (%t, %v)", started, err)
		}

		config.ProfileMutex = true
		settings := fakeRuntimeSettings(nil, nil)
		settings.setMutexProfileFraction = nil
		if started, err := startWithGate(config, new(startGate), validStarter(), settings); started || err == nil || !strings.Contains(err.Error(), "mutex runtime sampler") {
			t.Fatalf("nil mutex sampler = (%t, %v)", started, err)
		}
		config.ProfileMutex = false
		config.ProfileBlock = true
		settings = fakeRuntimeSettings(nil, nil)
		settings.setBlockProfileRate = nil
		if started, err := startWithGate(config, new(startGate), validStarter(), settings); started || err == nil || !strings.Contains(err.Error(), "block runtime sampler") {
			t.Fatalf("nil block sampler = (%t, %v)", started, err)
		}
	})

	t.Run("rejects nil SDK handle", func(t *testing.T) {
		started, err := startWith(validConfig(), func(pyroscope.Config) (profilerHandle, error) { return nil, nil })
		if started || err == nil || !strings.Contains(err.Error(), "nil profiler") {
			t.Fatalf("startWith() = (%t, %v), want nil handle error", started, err)
		}
	})
}

func TestStartGateLifecycle(t *testing.T) {
	config := validConfig()
	config.ProfileMutex = true
	config.MutexProfileFraction = 7
	config.ProfileBlock = true
	config.BlockProfileRate = 11

	var mutexRates []int
	var blockRates []int
	settings := runtimeSettings{
		setMutexProfileFraction: func(rate int) int {
			mutexRates = append(mutexRates, rate)
			return 3
		},
		setBlockProfileRate: func(rate int) { blockRates = append(blockRates, rate) },
	}
	handle := &fakeProfiler{}
	gate := new(startGate)

	if err := gate.stop(); !errors.Is(err, ErrNotStarted) {
		t.Fatalf("stop before start error = %v, want ErrNotStarted", err)
	}
	started, err := startWithGate(config, gate, func(pyroscope.Config) (profilerHandle, error) { return handle, nil }, settings)
	if err != nil || !started {
		t.Fatalf("start = (%t, %v), want success", started, err)
	}
	if !reflect.DeepEqual(mutexRates, []int{7}) || !reflect.DeepEqual(blockRates, []int{11}) {
		t.Fatalf("runtime rates after start: mutex=%v block=%v", mutexRates, blockRates)
	}
	started, err = startWithGate(config, gate, validStarter(), settings)
	if started || !errors.Is(err, ErrAlreadyStarted) {
		t.Fatalf("duplicate start = (%t, %v), want ErrAlreadyStarted", started, err)
	}
	if err := gate.stop(); err != nil {
		t.Fatalf("stop error = %v", err)
	}
	if handle.calls.Load() != 1 || handle.flushCalls.Load() != 1 || !handle.flushWait.Load() ||
		!reflect.DeepEqual(handle.eventsSnapshot(), []string{"flush", "stop"}) ||
		!reflect.DeepEqual(mutexRates, []int{7, 3}) || !reflect.DeepEqual(blockRates, []int{11, 0}) {
		t.Fatalf("stop state: calls=%d flush=%d wait=%t events=%v mutex=%v block=%v", handle.calls.Load(), handle.flushCalls.Load(), handle.flushWait.Load(), handle.eventsSnapshot(), mutexRates, blockRates)
	}
	if err := gate.stop(); err != nil || handle.calls.Load() != 1 || handle.flushCalls.Load() != 1 {
		t.Fatalf("idempotent stop = %v, calls=%d flush=%d", err, handle.calls.Load(), handle.flushCalls.Load())
	}
	started, err = startWithGate(config, gate, validStarter(), settings)
	if started || !errors.Is(err, ErrAlreadyStarted) {
		t.Fatalf("start after stop = (%t, %v), want ErrAlreadyStarted", started, err)
	}
}

func TestStartGateStopErrorIsStable(t *testing.T) {
	want := errors.New("flush failed")
	handle := &fakeProfiler{err: want}
	gate := new(startGate)
	if started, err := startWithGate(validConfig(), gate, func(pyroscope.Config) (profilerHandle, error) { return handle, nil }, fakeRuntimeSettings(nil, nil)); err != nil || !started {
		t.Fatalf("start = (%t, %v)", started, err)
	}
	for i := 0; i < 2; i++ {
		if err := gate.stop(); !errors.Is(err, want) {
			t.Fatalf("stop %d error = %v, want wrapped error", i, err)
		}
	}
	if handle.calls.Load() != 1 || handle.flushCalls.Load() != 1 || !handle.flushWait.Load() {
		t.Fatalf("Stop() calls=%d flush=%d wait=%t, want 1, 1, true", handle.calls.Load(), handle.flushCalls.Load(), handle.flushWait.Load())
	}
}

func TestStartGateConcurrentStart(t *testing.T) {
	const workers = 32
	gate := new(startGate)
	ready := make(chan struct{})
	results := make(chan startResult, workers)
	var wg sync.WaitGroup
	var calls atomic.Int32

	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-ready
			started, err := startWithGate(validConfig(), gate, func(pyroscope.Config) (profilerHandle, error) {
				calls.Add(1)
				return &fakeProfiler{}, nil
			}, fakeRuntimeSettings(nil, nil))
			results <- startResult{started: started, err: err}
		}()
	}
	close(ready)
	wg.Wait()
	close(results)

	successes, duplicates := 0, 0
	for result := range results {
		switch {
		case result.started && result.err == nil:
			successes++
		case !result.started && errors.Is(result.err, ErrAlreadyStarted):
			duplicates++
		default:
			t.Fatalf("result = (%t, %v)", result.started, result.err)
		}
	}
	if successes != 1 || duplicates != workers-1 || calls.Load() != 1 {
		t.Fatalf("successes=%d duplicates=%d calls=%d", successes, duplicates, calls.Load())
	}
}

func TestStartGateConcurrentStop(t *testing.T) {
	const workers = 32
	handle := &fakeProfiler{}
	gate := new(startGate)
	if started, err := startWithGate(validConfig(), gate, func(pyroscope.Config) (profilerHandle, error) { return handle, nil }, fakeRuntimeSettings(nil, nil)); err != nil || !started {
		t.Fatalf("start = (%t, %v)", started, err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- gate.stop()
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent stop error = %v", err)
		}
	}
	if handle.calls.Load() != 1 || handle.flushCalls.Load() != 1 {
		t.Fatalf("underlying Stop calls=%d Flush calls=%d, want 1 each", handle.calls.Load(), handle.flushCalls.Load())
	}
}

func TestStartGateConcurrentStartAndStop(t *testing.T) {
	gate := new(startGate)
	handle := &fakeProfiler{}
	starterEntered := make(chan struct{})
	releaseStarter := make(chan struct{})
	startDone := make(chan startResult, 1)
	stopDone := make(chan error, 1)

	go func() {
		started, err := startWithGate(validConfig(), gate, func(pyroscope.Config) (profilerHandle, error) {
			close(starterEntered)
			<-releaseStarter
			return handle, nil
		}, fakeRuntimeSettings(nil, nil))
		startDone <- startResult{started: started, err: err}
	}()
	<-starterEntered
	go func() { stopDone <- gate.stop() }()
	close(releaseStarter)

	result := <-startDone
	if !result.started || result.err != nil {
		t.Fatalf("concurrent start = (%t, %v), want success", result.started, result.err)
	}
	if err := <-stopDone; err != nil {
		t.Fatalf("concurrent stop error = %v", err)
	}
	if handle.calls.Load() != 1 || handle.flushCalls.Load() != 1 {
		t.Fatalf("underlying Stop calls=%d Flush calls=%d, want 1 each", handle.calls.Load(), handle.flushCalls.Load())
	}
}

func TestPublicEntrypointsWithoutStartingSDK(t *testing.T) {
	clearProfilerEnvironment(t)
	if started, err := Start(Config{}); started || err != nil {
		t.Fatalf("Start(disabled) = (%t, %v)", started, err)
	}
	if err := Stop(); !errors.Is(err, ErrNotStarted) {
		t.Fatalf("Stop() before process start = %v, want ErrNotStarted", err)
	}

	t.Setenv(EnvEnabled, "false")
	if started, err := StartFromEnv(); started || err != nil {
		t.Fatalf("StartFromEnv(disabled) = (%t, %v)", started, err)
	}
	t.Setenv(EnvEnabled, "not-a-boolean")
	if started, err := StartFromEnv(); started || err == nil || !strings.Contains(err.Error(), EnvEnabled) {
		t.Fatalf("StartFromEnv(invalid) = (%t, %v)", started, err)
	}

	t.Setenv(EnvAdhocServerAddress, "https://untrusted.example.test")
	if started, err := Start(validConfig()); started || err == nil || !strings.Contains(err.Error(), EnvAdhocServerAddress) {
		t.Fatalf("Start(override) = (%t, %v)", started, err)
	}
	if started, err := Start(Config{}); started || err != nil {
		t.Fatalf("Start(disabled with override) = (%t, %v)", started, err)
	}
}

func TestScalarParsers(t *testing.T) {
	booleanTests := []struct {
		name         string
		values       map[string]string
		defaultValue bool
		want         bool
		wantErr      bool
	}{
		{name: "absent false"},
		{name: "absent true", defaultValue: true, want: true},
		{name: "blank default", values: map[string]string{"VALUE": " \t"}, defaultValue: true, want: true},
		{name: "trim true", values: map[string]string{"VALUE": " TRUE "}, want: true},
		{name: "false", values: map[string]string{"VALUE": "false"}},
		{name: "invalid", values: map[string]string{"VALUE": "enabled"}, wantErr: true},
	}
	for _, test := range booleanTests {
		t.Run("bool "+test.name, func(t *testing.T) {
			got, err := boolFromEnv(mapLookup(test.values), "VALUE", test.defaultValue)
			if test.wantErr {
				if err == nil || !strings.Contains(err.Error(), "VALUE") {
					t.Fatalf("boolFromEnv() error = %v", err)
				}
				return
			}
			if err != nil || got != test.want {
				t.Fatalf("boolFromEnv() = (%t, %v), want %t", got, err, test.want)
			}
		})
	}

	if got, err := durationFromEnv(mapLookup(nil), "VALUE", time.Minute); err != nil || got != time.Minute {
		t.Fatalf("duration default = (%s, %v)", got, err)
	}
	if got, err := durationFromEnv(mapLookup(map[string]string{"VALUE": " 20s "}), "VALUE", 0); err != nil || got != 20*time.Second {
		t.Fatalf("duration parsed = (%s, %v)", got, err)
	}
	if _, err := durationFromEnv(mapLookup(map[string]string{"VALUE": "later"}), "VALUE", 0); err == nil {
		t.Fatal("durationFromEnv() accepted invalid duration")
	}
	if got, err := intFromEnv(mapLookup(nil), "VALUE", 5); err != nil || got != 5 {
		t.Fatalf("int default = (%d, %v)", got, err)
	}
	if got, err := intFromEnv(mapLookup(map[string]string{"VALUE": " 7 "}), "VALUE", 0); err != nil || got != 7 {
		t.Fatalf("int parsed = (%d, %v)", got, err)
	}
	if _, err := intFromEnv(mapLookup(map[string]string{"VALUE": "many"}), "VALUE", 0); err == nil {
		t.Fatal("intFromEnv() accepted invalid integer")
	}
	if got := stringFromEnv(mapLookup(map[string]string{"VALUE": " value "}), "VALUE"); got != "value" {
		t.Fatalf("stringFromEnv() = %q", got)
	}
	if got := stringFromEnv(mapLookup(nil), "VALUE"); got != "" {
		t.Fatalf("missing stringFromEnv() = %q", got)
	}
}

func TestNewHTTPClient(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://proxy.invalid:3128")
	t.Setenv("HTTPS_PROXY", "http://proxy.invalid:3128")
	t.Setenv("NO_PROXY", "")

	client := newHTTPClient(0)
	if client.Timeout != defaultHTTPTimeout {
		t.Fatalf("newHTTPClient(0) timeout = %s, want %s", client.Timeout, defaultHTTPTimeout)
	}
	boundedTransport, ok := client.Transport.(*boundedResponseTransport)
	if !ok || boundedTransport.maximumBytes != maximumResponseBodyBytes {
		t.Fatalf("newHTTPClient(0) transport = %#v, want %d-byte bounded transport", client.Transport, maximumResponseBodyBytes)
	}
	transport, ok := boundedTransport.base.(*http.Transport)
	if !ok || transport.MaxConnsPerHost != 5 || transport.MaxIdleConnsPerHost != 5 {
		t.Fatalf("newHTTPClient(0) base transport = %#v, want active and idle per-host limits of 5", boundedTransport.base)
	}
	if transport.Proxy != nil {
		t.Fatal("newHTTPClient(0) inherited proxy behavior from the process environment")
	}
	if transport.TLSClientConfig != nil {
		t.Fatal("newHTTPClient(0) replaced standard system-root TLS validation")
	}
	if transport.MaxResponseHeaderBytes != maximumResponseHeaderBytes {
		t.Fatalf("MaxResponseHeaderBytes = %d, want %d", transport.MaxResponseHeaderBytes, maximumResponseHeaderBytes)
	}

	client = newHTTPClient(2 * time.Second)
	if client.Timeout != 2*time.Second {
		t.Fatalf("newHTTPClient(2s) timeout = %s", client.Timeout)
	}
}

func TestBoundedResponseTransportAllowsNormalBodies(t *testing.T) {
	tests := []struct {
		name string
		size int
	}{
		{name: "small", size: 2},
		{name: "exact limit", size: maximumResponseBodyBytes},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			original := bytes.Repeat([]byte("a"), test.size)
			body := &closeTrackingReadCloser{Reader: bytes.NewReader(original)}
			transport := &boundedResponseTransport{
				base: roundTripFunc(func(*http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode:    http.StatusOK,
						Body:          body,
						ContentLength: int64(test.size),
					}, nil
				}),
				maximumBytes: maximumResponseBodyBytes,
			}

			request, err := http.NewRequest(http.MethodPost, "https://pyroscope.example.test/ingest", nil)
			if err != nil {
				t.Fatal(err)
			}
			response, err := transport.RoundTrip(request)
			if err != nil {
				t.Fatalf("RoundTrip() error = %v", err)
			}
			got, err := io.ReadAll(response.Body)
			if err != nil {
				t.Fatalf("ReadAll() error = %v", err)
			}
			if !bytes.Equal(got, original) {
				t.Fatalf("ReadAll() returned %d bytes, want %d", len(got), len(original))
			}
			if err := response.Body.Close(); err != nil {
				t.Fatalf("Close() error = %v", err)
			}
			if body.closeCalls != 1 {
				t.Fatalf("underlying Close() calls = %d, want 1", body.closeCalls)
			}
		})
	}
}

func TestBoundedResponseTransportRejectsOversizedBodies(t *testing.T) {
	tests := []struct {
		name             string
		contentLength    int64
		transferEncoding []string
	}{
		{
			name:          "declared content length",
			contentLength: maximumResponseBodyBytes + 1,
		},
		{
			name:             "chunked unknown length",
			contentLength:    -1,
			transferEncoding: []string{"chunked"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body := &closeTrackingReadCloser{
				Reader: bytes.NewReader(bytes.Repeat([]byte("b"), maximumResponseBodyBytes+1)),
			}
			transport := &boundedResponseTransport{
				base: roundTripFunc(func(*http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode:       http.StatusBadGateway,
						Body:             body,
						ContentLength:    test.contentLength,
						TransferEncoding: test.transferEncoding,
					}, nil
				}),
				maximumBytes: maximumResponseBodyBytes,
			}

			request, err := http.NewRequest(http.MethodPost, "https://pyroscope.example.test/ingest", nil)
			if err != nil {
				t.Fatal(err)
			}
			response, err := transport.RoundTrip(request)
			if err != nil {
				t.Fatalf("RoundTrip() error = %v", err)
			}
			got, err := io.ReadAll(response.Body)
			if !errors.Is(err, errResponseBodyTooLarge) {
				t.Fatalf("ReadAll() error = %v, want %v", err, errResponseBodyTooLarge)
			}
			if len(got) != maximumResponseBodyBytes {
				t.Fatalf("ReadAll() returned %d bytes before error, want %d", len(got), maximumResponseBodyBytes)
			}
			if _, err := response.Body.Read(make([]byte, 1)); !errors.Is(err, errResponseBodyTooLarge) {
				t.Fatalf("Read() after overflow error = %v, want deterministic size error", err)
			}
			if err := response.Body.Close(); err != nil {
				t.Fatalf("Close() error = %v", err)
			}
			if body.closeCalls != 1 {
				t.Fatalf("underlying Close() calls = %d, want 1", body.closeCalls)
			}
		})
	}
}

func TestBoundedResponseTransportLimitsDecompressedGzipBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Encoding", "gzip")
		writer := gzip.NewWriter(response)
		_, _ = writer.Write(bytes.Repeat([]byte("z"), maximumResponseBodyBytes+1))
		_ = writer.Close()
	}))
	defer server.Close()

	client := newHTTPClient(2 * time.Second)
	response, err := client.Get(server.URL)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if !response.Uncompressed {
		t.Fatal("response was not transparently decompressed before the body limiter")
	}
	if response.ContentLength != -1 {
		t.Fatalf("decompressed ContentLength = %d, want unknown length", response.ContentLength)
	}
	got, err := io.ReadAll(response.Body)
	if !errors.Is(err, errResponseBodyTooLarge) {
		t.Fatalf("ReadAll() gzip error = %v, want %v", err, errResponseBodyTooLarge)
	}
	if len(got) != maximumResponseBodyBytes {
		t.Fatalf("ReadAll() returned %d decompressed bytes, want %d", len(got), maximumResponseBodyBytes)
	}
}

func TestBoundedResponseTransportPreservesCloseError(t *testing.T) {
	closeErr := errors.New("close failed")
	body := &closeTrackingReadCloser{
		Reader:   strings.NewReader("response"),
		closeErr: closeErr,
	}
	transport := &boundedResponseTransport{
		base: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: body}, nil
		}),
		maximumBytes: maximumResponseBodyBytes,
	}
	request, err := http.NewRequest(http.MethodPost, "https://pyroscope.example.test/ingest", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
	if err := response.Body.Close(); !errors.Is(err, closeErr) {
		t.Fatalf("Close() error = %v, want %v", err, closeErr)
	}
	if body.closeCalls != 1 {
		t.Fatalf("underlying Close() calls = %d, want 1", body.closeCalls)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type closeTrackingReadCloser struct {
	io.Reader
	closeCalls int
	closeErr   error
}

func (b *closeTrackingReadCloser) Close() error {
	b.closeCalls++
	return b.closeErr
}

type startResult struct {
	started bool
	err     error
}

type fakeProfiler struct {
	calls      atomic.Int32
	flushCalls atomic.Int32
	flushWait  atomic.Bool
	err        error
	mu         sync.Mutex
	events     []string
}

func (f *fakeProfiler) Flush(wait bool) {
	f.flushCalls.Add(1)
	f.flushWait.Store(wait)
	f.mu.Lock()
	f.events = append(f.events, "flush")
	f.mu.Unlock()
}

func (f *fakeProfiler) Stop() error {
	f.calls.Add(1)
	f.mu.Lock()
	f.events = append(f.events, "stop")
	f.mu.Unlock()
	return f.err
}

func (f *fakeProfiler) eventsSnapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.events...)
}

func validConfig() Config {
	return Config{
		Enabled:              true,
		ServerAddress:        "https://pyroscope.example.test",
		ApplicationName:      "billing-api",
		UploadRate:           defaultUploadRate,
		HTTPTimeout:          defaultHTTPTimeout,
		ProfileCPU:           true,
		MutexProfileFraction: defaultMutexProfileFraction,
		BlockProfileRate:     defaultBlockProfileRate,
	}
}

func validEnvironment() map[string]string {
	return map[string]string{
		EnvEnabled:         "true",
		EnvServerAddress:   "https://pyroscope.example.test",
		EnvApplicationName: "billing-api",
	}
}

func disableAllProfiles(c *Config) {
	c.ProfileCPU = false
	c.ProfileAllocObjects = false
	c.ProfileAllocSpace = false
	c.ProfileInuseObjects = false
	c.ProfileInuseSpace = false
	c.ProfileGoroutines = false
	c.ProfileMutex = false
	c.ProfileBlock = false
}

func validStarter() starter {
	return func(pyroscope.Config) (profilerHandle, error) { return &fakeProfiler{}, nil }
}

func fakeRuntimeSettings(mutexRates, blockRates *[]int) runtimeSettings {
	return runtimeSettings{
		setMutexProfileFraction: func(rate int) int {
			if mutexRates != nil {
				*mutexRates = append(*mutexRates, rate)
			}
			return 0
		},
		setBlockProfileRate: func(rate int) {
			if blockRates != nil {
				*blockRates = append(*blockRates, rate)
			}
		},
	}
}

func mapLookup(values map[string]string) LookupEnv {
	return func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	}
}

func clearProfilerEnvironment(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		EnvEnabled,
		EnvServerAddress,
		EnvAllowInsecureHTTP,
		EnvApplicationName,
		EnvTags,
		EnvTenantID,
		EnvBasicAuthUser,
		EnvBasicAuthPassword,
		EnvBasicAuthPasswordFile,
		EnvLogging,
		EnvDisableGCRuns,
		EnvUploadRate,
		EnvHTTPTimeout,
		EnvProfileCPU,
		EnvProfileAllocObjects,
		EnvProfileAllocSpace,
		EnvProfileInuseObjects,
		EnvProfileInuseSpace,
		EnvProfileGoroutines,
		EnvProfileMutex,
		EnvProfileBlock,
		EnvMutexProfileFraction,
		EnvBlockProfileRate,
	} {
		t.Setenv(key, "")
	}
	value, present := os.LookupEnv(EnvAdhocServerAddress)
	if err := os.Unsetenv(EnvAdhocServerAddress); err != nil {
		t.Fatalf("unset %s: %v", EnvAdhocServerAddress, err)
	}
	t.Cleanup(func() {
		if present {
			_ = os.Setenv(EnvAdhocServerAddress, value)
		} else {
			_ = os.Unsetenv(EnvAdhocServerAddress)
		}
	})
}
