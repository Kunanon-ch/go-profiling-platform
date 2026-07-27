// Package profilerprobe provides a production-oriented bootstrap for the
// Grafana Pyroscope Go SDK. It uses push mode and intentionally does not
// register an HTTP pprof endpoint.
package profilerprobe

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	pyroscope "github.com/grafana/pyroscope-go"
)

const (
	// EnvEnabled controls whether continuous profiling starts. It defaults to false.
	EnvEnabled = "PYROSCOPE_ENABLED"

	// EnvServerAddress is the absolute HTTP(S) address of the self-hosted
	// Pyroscope server or an in-cluster ingestion proxy.
	EnvServerAddress = "PYROSCOPE_SERVER_ADDRESS"

	// EnvAllowInsecureHTTP explicitly permits plaintext HTTP for local or
	// isolated development environments. It defaults to false.
	EnvAllowInsecureHTTP = "PYROSCOPE_ALLOW_INSECURE_HTTP"

	// EnvAdhocServerAddress is an upstream SDK override that this wrapper
	// deliberately rejects because it bypasses validated destination policy.
	EnvAdhocServerAddress = "PYROSCOPE_ADHOC_SERVER_ADDRESS"

	// EnvApplicationName is the stable logical application name shown in Pyroscope.
	EnvApplicationName = "PYROSCOPE_APPLICATION_NAME"

	// EnvTags is a comma-separated list of static key=value labels.
	EnvTags = "PYROSCOPE_TAGS"

	// EnvTenantID is the optional tenant sent as X-Scope-OrgID.
	EnvTenantID = "PYROSCOPE_TENANT_ID"

	// EnvBasicAuthUser is the optional HTTP Basic authentication username.
	EnvBasicAuthUser = "PYROSCOPE_BASIC_AUTH_USER"

	// EnvBasicAuthPassword is the optional HTTP Basic authentication password.
	// Prefer EnvBasicAuthPasswordFile in orchestrated environments.
	EnvBasicAuthPassword = "PYROSCOPE_BASIC_AUTH_PASSWORD"

	// EnvBasicAuthPasswordFile points to a mounted file containing the HTTP
	// Basic authentication password.
	EnvBasicAuthPasswordFile = "PYROSCOPE_BASIC_AUTH_PASSWORD_FILE"

	// EnvLogging explicitly enables the SDK's standard diagnostic logger.
	EnvLogging = "PYROSCOPE_LOGGING"

	// EnvDisableGCRuns prevents the SDK from forcing garbage collection before
	// heap snapshots. It defaults to true to minimize application overhead.
	EnvDisableGCRuns = "PYROSCOPE_DISABLE_GC_RUNS"

	// EnvUploadRate controls how often profiles are uploaded.
	EnvUploadRate = "PYROSCOPE_UPLOAD_RATE"

	// EnvHTTPTimeout bounds each profile upload request, including shutdown flushes.
	EnvHTTPTimeout = "PYROSCOPE_HTTP_TIMEOUT"

	EnvProfileCPU           = "PYROSCOPE_PROFILE_CPU"
	EnvProfileAllocObjects  = "PYROSCOPE_PROFILE_ALLOC_OBJECTS"
	EnvProfileAllocSpace    = "PYROSCOPE_PROFILE_ALLOC_SPACE"
	EnvProfileInuseObjects  = "PYROSCOPE_PROFILE_INUSE_OBJECTS"
	EnvProfileInuseSpace    = "PYROSCOPE_PROFILE_INUSE_SPACE"
	EnvProfileGoroutines    = "PYROSCOPE_PROFILE_GOROUTINES"
	EnvProfileMutex         = "PYROSCOPE_PROFILE_MUTEX"
	EnvProfileBlock         = "PYROSCOPE_PROFILE_BLOCK"
	EnvMutexProfileFraction = "PYROSCOPE_MUTEX_PROFILE_FRACTION"
	EnvBlockProfileRate     = "PYROSCOPE_BLOCK_PROFILE_RATE"

	defaultUploadRate             = 15 * time.Second
	minimumUploadRate             = 10 * time.Second
	maximumUploadRate             = 5 * time.Minute
	defaultHTTPTimeout            = 5 * time.Second
	minimumHTTPTimeout            = 1 * time.Second
	maximumHTTPTimeout            = 30 * time.Second
	defaultMutexProfileFraction   = 5
	defaultBlockProfileRate       = 5
	maximumSecretFileBytes        = 64 * 1024
	maximumApplicationNameBytes   = 255
	maximumTags                   = 32
	maximumTagNameBytes           = 128
	maximumTagValueBytes          = 256
	maximumBasicAuthUserBytes     = 256
	maximumBasicAuthPasswordBytes = 4 * 1024
	maximumResponseBodyBytes      = 64 * 1024
	maximumResponseHeaderBytes    = 32 * 1024
)

var (
	applicationNamePattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.\-/]*$`)
	tagNamePattern         = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	tagValuePattern        = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:/@+\-]*$`)
	tenantIDPattern        = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

	// ErrAlreadyStarted reports that this package has already successfully
	// started a profiler in the current process, including one already stopped.
	ErrAlreadyStarted = errors.New("pyroscope profiler: already started")

	// ErrNotStarted reports that Stop was called before a successful Start.
	ErrNotStarted = errors.New("pyroscope profiler: not started")

	// errResponseBodyTooLarge prevents the upstream SDK from reading an
	// unbounded server or gateway response into application memory.
	errResponseBodyTooLarge = fmt.Errorf(
		"pyroscope profiler: response body exceeds %d bytes",
		maximumResponseBodyBytes,
	)
)

// Config controls a self-hosted Pyroscope profiling session. ApplicationName
// and low-cardinality Tags must remain stable across replicas of one logical
// application. Never put a pod UID, host name, request ID, or process ID in
// either field.
type Config struct {
	Enabled              bool
	ServerAddress        string
	AllowInsecureHTTP    bool
	ApplicationName      string
	Tags                 map[string]string
	TenantID             string
	BasicAuthUser        string
	BasicAuthPassword    string
	Logging              bool
	DisableGCRuns        bool
	UploadRate           time.Duration
	HTTPTimeout          time.Duration
	ProfileCPU           bool
	ProfileAllocObjects  bool
	ProfileAllocSpace    bool
	ProfileInuseObjects  bool
	ProfileInuseSpace    bool
	ProfileGoroutines    bool
	ProfileMutex         bool
	ProfileBlock         bool
	MutexProfileFraction int
	BlockProfileRate     int
}

// LookupEnv is compatible with os.LookupEnv and makes environment parsing
// deterministic in tests.
type LookupEnv func(string) (string, bool)

// Load reads Config from the process environment. Profiling is opt-in: when
// PYROSCOPE_ENABLED is absent or false, other profiler settings are ignored.
func Load() (Config, error) {
	return loadWith(os.LookupEnv, readSecretFile)
}

// LoadWithLookup is Load with an injectable environment lookup function.
// Password files, when configured, are still read from the local filesystem.
func LoadWithLookup(lookup LookupEnv) (Config, error) {
	return loadWith(lookup, readSecretFile)
}

type readFileFunc func(string) ([]byte, error)

func readSecretFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	return io.ReadAll(io.LimitReader(file, maximumSecretFileBytes+1))
}

func loadWith(lookup LookupEnv, readFile readFileFunc) (Config, error) {
	if lookup == nil {
		return Config{}, errors.New("pyroscope profiler: environment lookup is nil")
	}

	enabled, err := boolFromEnv(lookup, EnvEnabled, false)
	if err != nil {
		return Config{}, err
	}
	if !enabled {
		return Config{}, nil
	}
	if _, present := lookup(EnvAdhocServerAddress); present {
		return Config{}, fmt.Errorf("pyroscope profiler: %s is not allowed; use %s", EnvAdhocServerAddress, EnvServerAddress)
	}

	allowInsecureHTTP, err := boolFromEnv(lookup, EnvAllowInsecureHTTP, false)
	if err != nil {
		return Config{}, err
	}

	logging, err := boolFromEnv(lookup, EnvLogging, false)
	if err != nil {
		return Config{}, err
	}
	disableGCRuns, err := boolFromEnv(lookup, EnvDisableGCRuns, true)
	if err != nil {
		return Config{}, err
	}
	uploadRate, err := durationFromEnv(lookup, EnvUploadRate, defaultUploadRate)
	if err != nil {
		return Config{}, err
	}
	httpTimeout, err := durationFromEnv(lookup, EnvHTTPTimeout, defaultHTTPTimeout)
	if err != nil {
		return Config{}, err
	}

	profileCPU, err := boolFromEnv(lookup, EnvProfileCPU, true)
	if err != nil {
		return Config{}, err
	}
	profileAllocObjects, err := boolFromEnv(lookup, EnvProfileAllocObjects, false)
	if err != nil {
		return Config{}, err
	}
	profileAllocSpace, err := boolFromEnv(lookup, EnvProfileAllocSpace, true)
	if err != nil {
		return Config{}, err
	}
	profileInuseObjects, err := boolFromEnv(lookup, EnvProfileInuseObjects, false)
	if err != nil {
		return Config{}, err
	}
	profileInuseSpace, err := boolFromEnv(lookup, EnvProfileInuseSpace, true)
	if err != nil {
		return Config{}, err
	}
	profileGoroutines, err := boolFromEnv(lookup, EnvProfileGoroutines, true)
	if err != nil {
		return Config{}, err
	}
	profileMutex, err := boolFromEnv(lookup, EnvProfileMutex, false)
	if err != nil {
		return Config{}, err
	}
	profileBlock, err := boolFromEnv(lookup, EnvProfileBlock, false)
	if err != nil {
		return Config{}, err
	}
	mutexProfileFraction, err := intFromEnv(lookup, EnvMutexProfileFraction, defaultMutexProfileFraction)
	if err != nil {
		return Config{}, err
	}
	blockProfileRate, err := intFromEnv(lookup, EnvBlockProfileRate, defaultBlockProfileRate)
	if err != nil {
		return Config{}, err
	}

	tags, err := tagsFromEnv(lookup)
	if err != nil {
		return Config{}, err
	}
	password, err := basicAuthPasswordFromEnv(lookup, readFile)
	if err != nil {
		return Config{}, err
	}

	config := Config{
		Enabled:              true,
		ServerAddress:        stringFromEnv(lookup, EnvServerAddress),
		AllowInsecureHTTP:    allowInsecureHTTP,
		ApplicationName:      stringFromEnv(lookup, EnvApplicationName),
		Tags:                 tags,
		TenantID:             stringFromEnv(lookup, EnvTenantID),
		BasicAuthUser:        stringFromEnv(lookup, EnvBasicAuthUser),
		BasicAuthPassword:    password,
		Logging:              logging,
		DisableGCRuns:        disableGCRuns,
		UploadRate:           uploadRate,
		HTTPTimeout:          httpTimeout,
		ProfileCPU:           profileCPU,
		ProfileAllocObjects:  profileAllocObjects,
		ProfileAllocSpace:    profileAllocSpace,
		ProfileInuseObjects:  profileInuseObjects,
		ProfileInuseSpace:    profileInuseSpace,
		ProfileGoroutines:    profileGoroutines,
		ProfileMutex:         profileMutex,
		ProfileBlock:         profileBlock,
		MutexProfileFraction: mutexProfileFraction,
		BlockProfileRate:     blockProfileRate,
	}

	return config, config.Validate()
}

// Validate checks all configuration that can be verified before the SDK starts.
func (c Config) Validate() error {
	if !c.Enabled {
		return nil
	}

	if err := validateServerAddress(c.ServerAddress, c.AllowInsecureHTTP); err != nil {
		return err
	}
	if c.ApplicationName == "" {
		return fmt.Errorf("pyroscope profiler: %s is required when %s=true", EnvApplicationName, EnvEnabled)
	}
	if len(c.ApplicationName) > maximumApplicationNameBytes || !applicationNamePattern.MatchString(c.ApplicationName) {
		return fmt.Errorf(
			"pyroscope profiler: %s must be at most %d bytes and match %s",
			EnvApplicationName,
			maximumApplicationNameBytes,
			applicationNamePattern.String(),
		)
	}
	if err := validateTags(c.Tags); err != nil {
		return err
	}
	if c.TenantID != "" && !tenantIDPattern.MatchString(c.TenantID) {
		return fmt.Errorf("pyroscope profiler: %s must match %s", EnvTenantID, tenantIDPattern.String())
	}
	if (c.BasicAuthUser == "") != (c.BasicAuthPassword == "") {
		return fmt.Errorf(
			"pyroscope profiler: %s and a password from %s or %s must be configured together",
			EnvBasicAuthUser,
			EnvBasicAuthPassword,
			EnvBasicAuthPasswordFile,
		)
	}
	if c.BasicAuthUser != "" {
		if len(c.BasicAuthUser) > maximumBasicAuthUserBytes || strings.ContainsAny(c.BasicAuthUser, ":\r\n\x00") {
			return fmt.Errorf("pyroscope profiler: %s contains unsupported characters or exceeds %d bytes", EnvBasicAuthUser, maximumBasicAuthUserBytes)
		}
		if len(c.BasicAuthPassword) > maximumBasicAuthPasswordBytes || strings.ContainsAny(c.BasicAuthPassword, "\r\n\x00") {
			return fmt.Errorf("pyroscope profiler: password from %s or %s contains unsupported characters or exceeds %d bytes", EnvBasicAuthPassword, EnvBasicAuthPasswordFile, maximumBasicAuthPasswordBytes)
		}
	}
	if c.UploadRate != 0 && (c.UploadRate < minimumUploadRate || c.UploadRate > maximumUploadRate) {
		return fmt.Errorf(
			"pyroscope profiler: %s must be between %s and %s",
			EnvUploadRate,
			minimumUploadRate,
			maximumUploadRate,
		)
	}
	if c.HTTPTimeout != 0 && (c.HTTPTimeout < minimumHTTPTimeout || c.HTTPTimeout > maximumHTTPTimeout) {
		return fmt.Errorf(
			"pyroscope profiler: %s must be between %s and %s",
			EnvHTTPTimeout,
			minimumHTTPTimeout,
			maximumHTTPTimeout,
		)
	}
	if !c.hasProfileType() {
		return errors.New("pyroscope profiler: at least one profile type must be enabled")
	}
	if c.ProfileMutex && c.MutexProfileFraction <= 0 {
		return fmt.Errorf("pyroscope profiler: %s must be greater than zero when %s=true", EnvMutexProfileFraction, EnvProfileMutex)
	}
	if c.ProfileBlock && c.BlockProfileRate <= 0 {
		return fmt.Errorf("pyroscope profiler: %s must be greater than zero when %s=true", EnvBlockProfileRate, EnvProfileBlock)
	}

	return nil
}

func (c Config) hasProfileType() bool {
	return c.ProfileCPU || c.ProfileAllocObjects || c.ProfileAllocSpace ||
		c.ProfileInuseObjects || c.ProfileInuseSpace || c.ProfileGoroutines ||
		c.ProfileMutex || c.ProfileBlock
}

// pyroscopeConfig deliberately remains private. Callers must go through Start
// so opt-in, validation, runtime sampling, and the process-wide gate cannot be
// bypassed accidentally.
func (c Config) pyroscopeConfig() pyroscope.Config {
	profileTypes := make([]pyroscope.ProfileType, 0, 11)
	if c.ProfileCPU {
		profileTypes = append(profileTypes, pyroscope.ProfileCPU)
	}
	if c.ProfileAllocObjects {
		profileTypes = append(profileTypes, pyroscope.ProfileAllocObjects)
	}
	if c.ProfileAllocSpace {
		profileTypes = append(profileTypes, pyroscope.ProfileAllocSpace)
	}
	if c.ProfileInuseObjects {
		profileTypes = append(profileTypes, pyroscope.ProfileInuseObjects)
	}
	if c.ProfileInuseSpace {
		profileTypes = append(profileTypes, pyroscope.ProfileInuseSpace)
	}
	if c.ProfileGoroutines {
		profileTypes = append(profileTypes, pyroscope.ProfileGoroutines)
	}
	if c.ProfileMutex {
		profileTypes = append(profileTypes, pyroscope.ProfileMutexCount, pyroscope.ProfileMutexDuration)
	}
	if c.ProfileBlock {
		profileTypes = append(profileTypes, pyroscope.ProfileBlockCount, pyroscope.ProfileBlockDuration)
	}

	var logger pyroscope.Logger
	if c.Logging {
		logger = pyroscope.StandardLogger
	}

	return pyroscope.Config{
		ApplicationName:   c.ApplicationName,
		Tags:              cloneTags(c.Tags),
		ServerAddress:     c.ServerAddress,
		BasicAuthUser:     c.BasicAuthUser,
		BasicAuthPassword: c.BasicAuthPassword,
		TenantID:          c.TenantID,
		UploadRate:        c.UploadRate,
		HTTPClient:        newHTTPClient(c.HTTPTimeout),
		Logger:            logger,
		ProfileTypes:      profileTypes,
		DisableGCRuns:     c.DisableGCRuns,
	}
}

// Start starts push-based continuous profiling when enabled. It does not open
// an HTTP listener. Exactly one successful start is allowed for the process;
// failed attempts may be retried. The caller chooses whether a startup error is
// fatal to the application.
func Start(c Config) (bool, error) {
	if c.Enabled {
		if _, present := os.LookupEnv(EnvAdhocServerAddress); present {
			return false, fmt.Errorf("pyroscope profiler: %s is not allowed; use %s", EnvAdhocServerAddress, EnvServerAddress)
		}
	}
	return startWithGate(c, &processGate, func(config pyroscope.Config) (profilerHandle, error) {
		return pyroscope.Start(config)
	}, processRuntimeSettings)
}

// StartFromEnv loads Config from the process environment and starts profiling.
// A false, nil result means profiling was disabled.
func StartFromEnv() (bool, error) {
	config, err := Load()
	if err != nil {
		return false, err
	}
	return Start(config)
}

// Stop synchronously snapshots the final interval with Flush(true), waits for
// queued uploads, and then stops the SDK. It can therefore block during a slow
// backend or network and should run within the application's graceful-shutdown
// budget. Stop is safe to call concurrently and is idempotent after a successful
// start. A process cannot start a second profiler after Stop.
func Stop() error {
	return processGate.stop()
}

type profilerHandle interface {
	Flush(bool)
	Stop() error
}

type starter func(pyroscope.Config) (profilerHandle, error)

type runtimeSettings struct {
	setMutexProfileFraction func(int) int
	setBlockProfileRate     func(int)
}

var (
	processGate            = startGate{}
	processRuntimeSettings = runtimeSettings{
		setMutexProfileFraction: runtime.SetMutexProfileFraction,
		setBlockProfileRate:     runtime.SetBlockProfileRate,
	}
)

// startGate serializes SDK startup and shutdown. A failed start does not
// consume the gate, while a successful start remains consumed after Stop.
type startGate struct {
	mu                    sync.Mutex
	started               bool
	stopped               bool
	handle                profilerHandle
	stopErr               error
	runtimeSettings       runtimeSettings
	mutexSamplingChanged  bool
	previousMutexFraction int
	blockSamplingChanged  bool
}

func startWith(c Config, start starter) (bool, error) {
	settings := runtimeSettings{
		setMutexProfileFraction: func(int) int { return 0 },
		setBlockProfileRate:     func(int) {},
	}
	return startWithGate(c, new(startGate), start, settings)
}

func startWithGate(c Config, gate *startGate, start starter, settings runtimeSettings) (bool, error) {
	if err := c.Validate(); err != nil {
		return false, err
	}
	if !c.Enabled {
		return false, nil
	}
	if gate == nil {
		return false, errors.New("pyroscope profiler: start gate is nil")
	}
	if start == nil {
		return false, errors.New("pyroscope profiler: starter is nil")
	}
	if c.ProfileMutex && settings.setMutexProfileFraction == nil {
		return false, errors.New("pyroscope profiler: mutex runtime sampler is nil")
	}
	if c.ProfileBlock && settings.setBlockProfileRate == nil {
		return false, errors.New("pyroscope profiler: block runtime sampler is nil")
	}

	return gate.startValidated(c, start, settings)
}

func (g *startGate) startValidated(c Config, start starter, settings runtimeSettings) (bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.started {
		return false, ErrAlreadyStarted
	}
	handle, err := start(c.pyroscopeConfig())
	if err != nil {
		return false, fmt.Errorf("pyroscope profiler: start SDK: %w", err)
	}
	if handle == nil {
		return false, errors.New("pyroscope profiler: SDK returned a nil profiler")
	}

	g.started = true
	g.handle = handle
	g.runtimeSettings = settings
	if c.ProfileMutex {
		g.previousMutexFraction = settings.setMutexProfileFraction(c.MutexProfileFraction)
		g.mutexSamplingChanged = true
	}
	if c.ProfileBlock {
		settings.setBlockProfileRate(c.BlockProfileRate)
		g.blockSamplingChanged = true
	}

	return true, nil
}

func (g *startGate) stop() error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if !g.started {
		return ErrNotStarted
	}
	if g.stopped {
		return g.stopErr
	}

	g.handle.Flush(true)
	err := g.handle.Stop()
	if g.blockSamplingChanged {
		g.runtimeSettings.setBlockProfileRate(0)
	}
	if g.mutexSamplingChanged {
		g.runtimeSettings.setMutexProfileFraction(g.previousMutexFraction)
	}

	g.stopped = true
	g.handle = nil
	if err != nil {
		g.stopErr = fmt.Errorf("pyroscope profiler: stop SDK: %w", err)
	}
	return g.stopErr
}

func boolFromEnv(lookup LookupEnv, key string, defaultValue bool) (bool, error) {
	raw, ok := lookup(key)
	if !ok || strings.TrimSpace(raw) == "" {
		return defaultValue, nil
	}

	value, err := strconv.ParseBool(strings.TrimSpace(raw))
	if err != nil {
		return false, fmt.Errorf("pyroscope profiler: %s must be a boolean: %w", key, err)
	}
	return value, nil
}

func durationFromEnv(lookup LookupEnv, key string, defaultValue time.Duration) (time.Duration, error) {
	raw, ok := lookup(key)
	if !ok || strings.TrimSpace(raw) == "" {
		return defaultValue, nil
	}

	value, err := time.ParseDuration(strings.TrimSpace(raw))
	if err != nil {
		return 0, fmt.Errorf("pyroscope profiler: %s must be a duration: %w", key, err)
	}
	return value, nil
}

func intFromEnv(lookup LookupEnv, key string, defaultValue int) (int, error) {
	raw, ok := lookup(key)
	if !ok || strings.TrimSpace(raw) == "" {
		return defaultValue, nil
	}

	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, fmt.Errorf("pyroscope profiler: %s must be an integer: %w", key, err)
	}
	return value, nil
}

func stringFromEnv(lookup LookupEnv, key string) string {
	value, ok := lookup(key)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}

func tagsFromEnv(lookup LookupEnv) (map[string]string, error) {
	raw := stringFromEnv(lookup, EnvTags)
	if raw == "" {
		return nil, nil
	}

	tags := make(map[string]string)
	for _, entry := range strings.Split(raw, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" || strings.Count(entry, "=") != 1 {
			return nil, fmt.Errorf("pyroscope profiler: %s must be a comma-separated key=value list", EnvTags)
		}
		key, value, _ := strings.Cut(entry, "=")
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			return nil, fmt.Errorf("pyroscope profiler: %s entries require non-empty keys and values", EnvTags)
		}
		if _, exists := tags[key]; exists {
			return nil, fmt.Errorf("pyroscope profiler: %s contains duplicate key %q", EnvTags, key)
		}
		tags[key] = value
	}
	if err := validateTags(tags); err != nil {
		return nil, err
	}
	return tags, nil
}

func basicAuthPasswordFromEnv(lookup LookupEnv, readFile readFileFunc) (string, error) {
	password, passwordSet := lookup(EnvBasicAuthPassword)
	passwordSet = passwordSet && password != ""
	passwordFile := stringFromEnv(lookup, EnvBasicAuthPasswordFile)
	if passwordSet && passwordFile != "" {
		return "", fmt.Errorf("pyroscope profiler: %s and %s are mutually exclusive", EnvBasicAuthPassword, EnvBasicAuthPasswordFile)
	}
	if passwordSet {
		return password, nil
	}
	if passwordFile == "" {
		return "", nil
	}
	if readFile == nil {
		return "", errors.New("pyroscope profiler: password file reader is nil")
	}

	contents, err := readFile(passwordFile)
	if err != nil {
		return "", fmt.Errorf("pyroscope profiler: read %s: %w", EnvBasicAuthPasswordFile, err)
	}
	if len(contents) > maximumSecretFileBytes {
		return "", fmt.Errorf("pyroscope profiler: %s exceeds %d bytes", EnvBasicAuthPasswordFile, maximumSecretFileBytes)
	}
	password = strings.TrimSuffix(string(contents), "\n")
	password = strings.TrimSuffix(password, "\r")
	if password == "" {
		return "", fmt.Errorf("pyroscope profiler: %s is empty", EnvBasicAuthPasswordFile)
	}
	return password, nil
}

func validateServerAddress(address string, allowInsecureHTTP bool) error {
	if address == "" {
		return fmt.Errorf("pyroscope profiler: %s is required when %s=true", EnvServerAddress, EnvEnabled)
	}
	parsed, err := url.Parse(address)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.Hostname() == "" || parsed.Opaque != "" {
		return fmt.Errorf("pyroscope profiler: %s must be an absolute HTTP(S) URL", EnvServerAddress)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("pyroscope profiler: %s must use http or https", EnvServerAddress)
	}
	if parsed.Scheme == "http" && !allowInsecureHTTP {
		return fmt.Errorf("pyroscope profiler: %s must use https unless %s=true", EnvServerAddress, EnvAllowInsecureHTTP)
	}
	if parsed.User != nil {
		return fmt.Errorf("pyroscope profiler: %s must not contain credentials; use Basic Auth settings", EnvServerAddress)
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("pyroscope profiler: %s must not contain a query or fragment", EnvServerAddress)
	}
	return nil
}

func validateTags(tags map[string]string) error {
	if len(tags) > maximumTags {
		return fmt.Errorf("pyroscope profiler: %s supports at most %d tags", EnvTags, maximumTags)
	}
	for key, value := range tags {
		if len(key) > maximumTagNameBytes || key == "__name__" || !tagNamePattern.MatchString(key) {
			return fmt.Errorf("pyroscope profiler: %s contains invalid or reserved key %q", EnvTags, key)
		}
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("pyroscope profiler: %s contains an empty value for key %q", EnvTags, key)
		}
		if len(value) > maximumTagValueBytes || !tagValuePattern.MatchString(value) {
			return fmt.Errorf(
				"pyroscope profiler: %s value for key %q must be at most %d bytes and match %s",
				EnvTags,
				key,
				maximumTagValueBytes,
				tagValuePattern.String(),
			)
		}
	}
	return nil
}

func cloneTags(tags map[string]string) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	clone := make(map[string]string, len(tags))
	for key, value := range tags {
		clone[key] = value
	}
	return clone
}

func newHTTPClient(timeout time.Duration) *http.Client {
	if timeout == 0 {
		timeout = defaultHTTPTimeout
	}

	// Use a dedicated direct transport instead of http.DefaultTransport. In
	// particular, Proxy remains nil so process-wide HTTP_PROXY/HTTPS_PROXY
	// variables cannot reroute profiling data or credentials. A nil
	// TLSClientConfig keeps Go's standard system-root certificate validation.
	transport := &http.Transport{
		Proxy:                  nil,
		DialContext:            (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:      true,
		DisableCompression:     false,
		MaxIdleConns:           100,
		MaxIdleConnsPerHost:    5, // Retains one connection per SDK upload worker.
		MaxConnsPerHost:        5, // Matches pyroscope-go's upload worker count.
		IdleConnTimeout:        90 * time.Second,
		TLSHandshakeTimeout:    10 * time.Second,
		ExpectContinueTimeout:  1 * time.Second,
		MaxResponseHeaderBytes: maximumResponseHeaderBytes,
	}

	return &http.Client{
		// net/http transparently decompresses gzip responses inside Transport.
		// Applying the body limit outside it therefore bounds the bytes the SDK
		// actually reads, not merely their compressed representation.
		Transport: &boundedResponseTransport{
			base:         transport,
			maximumBytes: maximumResponseBodyBytes,
		},
		Timeout: timeout,
		// Never follow ingestion redirects. This preserves the validated
		// destination and prevents profiles or credentials crossing hosts.
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// boundedResponseTransport caps every successful RoundTrip response before it
// reaches pyroscope-go. The SDK reads response bodies with io.ReadAll, including
// error responses, so this boundary must be enforced by its dedicated client.
type boundedResponseTransport struct {
	base         http.RoundTripper
	maximumBytes int64
}

func (t *boundedResponseTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := t.base.RoundTrip(request)
	if err != nil || response == nil || response.Body == nil {
		return response, err
	}

	response.Body = &boundedResponseBody{
		body:      response.Body,
		remaining: t.maximumBytes,
	}
	return response, nil
}

type boundedResponseBody struct {
	body      io.ReadCloser
	remaining int64
	exceeded  bool
}

func (b *boundedResponseBody) Read(buffer []byte) (int, error) {
	if len(buffer) == 0 {
		return 0, nil
	}
	if b.exceeded {
		return 0, errResponseBodyTooLarge
	}

	if b.remaining > 0 {
		if int64(len(buffer)) > b.remaining {
			buffer = buffer[:b.remaining]
		}
		read, err := b.body.Read(buffer)
		b.remaining -= int64(read)
		return read, err
	}

	// Probe one byte beyond the limit. EOF proves that a response whose size is
	// exactly the limit is valid; any returned byte makes the failure explicit.
	var probe [1]byte
	read, err := b.body.Read(probe[:])
	if read > 0 {
		b.exceeded = true
		return 0, errResponseBodyTooLarge
	}
	return 0, err
}

func (b *boundedResponseBody) Close() error {
	return b.body.Close()
}
