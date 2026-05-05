package config

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
)

const (
	EnvUniFiTalkSIPServer    = "UNIFI_TALK_SIP_SERVER"
	EnvUniFiTalkSIPUsername  = "UNIFI_TALK_SIP_USERNAME"
	EnvUniFiTalkSIPPassword  = "UNIFI_TALK_SIP_PASSWORD"
	EnvUniFiTalkSIPExtension = "UNIFI_TALK_SIP_EXTENSION"
	EnvSIPTransport          = "SIP_TRANSPORT"
	EnvSIPBindAddr           = "SIP_BIND_ADDR"
	EnvSIPAdvertiseAddr      = "SIP_ADVERTISE_ADDR"
	EnvRTPPortMin            = "RTP_PORT_MIN"
	EnvRTPPortMax            = "RTP_PORT_MAX"
	EnvBridgeWSAddr          = "BRIDGE_WS_ADDR"
	EnvMetricsAddr           = "METRICS_ADDR"
	EnvCodecs                = "CODECS"
)

const (
	DefaultSIPTransport     = "udp"
	DefaultSIPBindAddr      = "0.0.0.0:5060"
	DefaultSIPAdvertiseAddr = "127.0.0.1:5060"
	DefaultRTPPortMin       = 10000
	DefaultRTPPortMax       = 10019
	DefaultBridgeWSAddr     = "127.0.0.1:9077"
	DefaultMetricsAddr      = "127.0.0.1:9078"
	DefaultCodecs           = "g711_ulaw"
)

var (
	allEnvNames = []string{
		EnvUniFiTalkSIPServer,
		EnvUniFiTalkSIPUsername,
		EnvUniFiTalkSIPPassword,
		EnvUniFiTalkSIPExtension,
		EnvSIPTransport,
		EnvSIPBindAddr,
		EnvSIPAdvertiseAddr,
		EnvRTPPortMin,
		EnvRTPPortMax,
		EnvBridgeWSAddr,
		EnvMetricsAddr,
		EnvCodecs,
	}
	sensitiveEnvNames = map[string]bool{
		EnvUniFiTalkSIPServer:    true,
		EnvUniFiTalkSIPUsername:  true,
		EnvUniFiTalkSIPPassword:  true,
		EnvUniFiTalkSIPExtension: true,
	}
)

// Address is a validated host:port endpoint from environment configuration.
type Address struct {
	Host string
	Port int
}

func (a Address) String() string {
	return net.JoinHostPort(a.Host, strconv.Itoa(a.Port))
}

// Config is the complete bridge runtime configuration. It is intentionally
// environment-only so credentials never need to be copied into files.
type Config struct {
	UniFiTalkSIPServer    string
	UniFiTalkSIPUsername  string
	UniFiTalkSIPPassword  string
	UniFiTalkSIPExtension string

	SIPTransport     string
	SIPBindAddr      Address
	SIPAdvertiseAddr Address
	RTPPortMin       int
	RTPPortMax       int
	BridgeWSAddr     Address
	MetricsAddr      Address
	Codecs           []string

	set map[string]bool
}

// RedactedValue is safe to emit to logs or status endpoints.
type RedactedValue struct {
	Name     string `json:"name"`
	Value    string `json:"value"`
	Set      bool   `json:"set"`
	Redacted bool   `json:"redacted"`
}

// ValidationError reports all invalid environment values together.
type ValidationError struct {
	Problems []string
}

func (e *ValidationError) Error() string {
	return "invalid bridge environment: " + strings.Join(e.Problems, "; ")
}

// ParseEnv loads bridge configuration from an os.Environ-style slice.
func ParseEnv(environ []string) (Config, error) {
	values := map[string]string{}
	set := map[string]bool{}
	for _, entry := range environ {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		values[key] = value
		set[key] = true
	}

	return ParseLookup(func(name string) (string, bool) {
		value, ok := values[name]
		return value, ok
	})
}

// ParseLookup loads bridge configuration from a lookup function such as os.LookupEnv.
func ParseLookup(lookup func(string) (string, bool)) (Config, error) {
	cfg := Config{set: map[string]bool{}}

	cfg.UniFiTalkSIPServer = readString(lookup, cfg.set, EnvUniFiTalkSIPServer, "")
	cfg.UniFiTalkSIPUsername = readString(lookup, cfg.set, EnvUniFiTalkSIPUsername, "")
	cfg.UniFiTalkSIPPassword = readString(lookup, cfg.set, EnvUniFiTalkSIPPassword, "")
	cfg.UniFiTalkSIPExtension = readString(lookup, cfg.set, EnvUniFiTalkSIPExtension, "")
	cfg.SIPTransport = strings.ToLower(readString(lookup, cfg.set, EnvSIPTransport, DefaultSIPTransport))
	cfg.RTPPortMin = readInt(lookup, cfg.set, EnvRTPPortMin, DefaultRTPPortMin)
	cfg.RTPPortMax = readInt(lookup, cfg.set, EnvRTPPortMax, DefaultRTPPortMax)

	var problems []string
	var err error

	cfg.SIPBindAddr, err = parseIPHostPort(readString(lookup, cfg.set, EnvSIPBindAddr, DefaultSIPBindAddr), EnvSIPBindAddr)
	if err != nil {
		problems = append(problems, err.Error())
	}
	cfg.SIPAdvertiseAddr, err = parseIPHostPort(readString(lookup, cfg.set, EnvSIPAdvertiseAddr, DefaultSIPAdvertiseAddr), EnvSIPAdvertiseAddr)
	if err != nil {
		problems = append(problems, err.Error())
	}
	cfg.BridgeWSAddr, err = parseHostPort(readString(lookup, cfg.set, EnvBridgeWSAddr, DefaultBridgeWSAddr), EnvBridgeWSAddr)
	if err != nil {
		problems = append(problems, err.Error())
	}
	cfg.MetricsAddr, err = parseHostPort(readString(lookup, cfg.set, EnvMetricsAddr, DefaultMetricsAddr), EnvMetricsAddr)
	if err != nil {
		problems = append(problems, err.Error())
	}
	cfg.Codecs, err = parseCodecs(readString(lookup, cfg.set, EnvCodecs, DefaultCodecs))
	if err != nil {
		problems = append(problems, err.Error())
	}

	problems = append(problems, validate(cfg)...)
	if len(problems) > 0 {
		return cfg, &ValidationError{Problems: problems}
	}
	return cfg, nil
}

func (c Config) UniFiConfigured() bool {
	return c.UniFiTalkSIPServer != "" &&
		c.UniFiTalkSIPUsername != "" &&
		c.UniFiTalkSIPPassword != "" &&
		c.UniFiTalkSIPExtension != ""
}

func (c Config) RedactedValues() []RedactedValue {
	values := map[string]string{
		EnvUniFiTalkSIPServer:    c.redactedSensitiveValue(EnvUniFiTalkSIPServer, c.UniFiTalkSIPServer),
		EnvUniFiTalkSIPUsername:  c.redactedSensitiveValue(EnvUniFiTalkSIPUsername, c.UniFiTalkSIPUsername),
		EnvUniFiTalkSIPPassword:  c.redactedSensitiveValue(EnvUniFiTalkSIPPassword, c.UniFiTalkSIPPassword),
		EnvUniFiTalkSIPExtension: c.redactedSensitiveValue(EnvUniFiTalkSIPExtension, c.UniFiTalkSIPExtension),
		EnvSIPTransport:          c.SIPTransport,
		EnvSIPBindAddr:           c.SIPBindAddr.String(),
		EnvSIPAdvertiseAddr:      c.SIPAdvertiseAddr.String(),
		EnvRTPPortMin:            strconv.Itoa(c.RTPPortMin),
		EnvRTPPortMax:            strconv.Itoa(c.RTPPortMax),
		EnvBridgeWSAddr:          c.BridgeWSAddr.String(),
		EnvMetricsAddr:           c.MetricsAddr.String(),
		EnvCodecs:                strings.Join(c.Codecs, ","),
	}

	result := make([]RedactedValue, 0, len(allEnvNames))
	for _, name := range allEnvNames {
		result = append(result, RedactedValue{
			Name:     name,
			Value:    values[name],
			Set:      c.set[name],
			Redacted: sensitiveEnvNames[name],
		})
	}
	return result
}

func (c Config) RedactedMap() map[string]string {
	result := map[string]string{}
	for _, item := range c.RedactedValues() {
		result[item.Name] = item.Value
	}
	return result
}

func (c Config) redactedSensitiveValue(name string, value string) string {
	if c.set[name] || value != "" {
		return "(set)"
	}
	return "(unset)"
}

func readString(lookup func(string) (string, bool), set map[string]bool, name string, fallback string) string {
	value, ok := lookup(name)
	if !ok {
		return fallback
	}
	set[name] = true
	if name == EnvUniFiTalkSIPPassword {
		return value
	}
	return strings.TrimSpace(value)
}

func readInt(lookup func(string) (string, bool), set map[string]bool, name string, fallback int) int {
	value, ok := lookup(name)
	if !ok {
		return fallback
	}
	set[name] = true
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return -1
	}
	return parsed
}

func parseIPHostPort(value string, name string) (Address, error) {
	addr, err := parseHostPort(value, name)
	if err != nil {
		return Address{}, err
	}
	if net.ParseIP(addr.Host) == nil {
		return Address{}, fmt.Errorf("%s host must be an IP address", name)
	}
	return addr, nil
}

func parseHostPort(value string, name string) (Address, error) {
	host, portText, err := net.SplitHostPort(value)
	if err != nil {
		return Address{}, fmt.Errorf("%s must be host:port: %w", name, err)
	}
	if strings.TrimSpace(host) == "" {
		return Address{}, fmt.Errorf("%s host is required", name)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		return Address{}, fmt.Errorf("%s port must be numeric", name)
	}
	if port < 1 || port > 65535 {
		return Address{}, fmt.Errorf("%s port must be 1-65535", name)
	}
	return Address{Host: host, Port: port}, nil
}

func parseCodecs(value string) ([]string, error) {
	allowed := map[string]bool{
		"g711_ulaw": true,
	}
	seen := map[string]bool{}
	var codecs []string
	for _, part := range strings.Split(value, ",") {
		codec := strings.ToLower(strings.TrimSpace(part))
		if codec == "" {
			return nil, errors.New("CODECS must not contain empty entries")
		}
		if !allowed[codec] {
			return nil, fmt.Errorf("CODECS contains unsupported codec %q", codec)
		}
		if seen[codec] {
			continue
		}
		seen[codec] = true
		codecs = append(codecs, codec)
	}
	if len(codecs) != 1 || !seen["g711_ulaw"] {
		return nil, errors.New("CODECS must be g711_ulaw for the POC")
	}
	return codecs, nil
}

func validate(c Config) []string {
	var problems []string
	if c.SIPTransport != "udp" {
		problems = append(problems, "SIP_TRANSPORT must be udp")
	}
	if c.RTPPortMin < 1024 || c.RTPPortMin > 65535 {
		problems = append(problems, "RTP_PORT_MIN must be 1024-65535")
	}
	if c.RTPPortMax < 1024 || c.RTPPortMax > 65535 {
		problems = append(problems, "RTP_PORT_MAX must be 1024-65535")
	}
	if c.RTPPortMin >= c.RTPPortMax {
		problems = append(problems, "RTP_PORT_MIN must be < RTP_PORT_MAX")
	}
	if c.RTPPortMin%2 != 0 {
		problems = append(problems, "RTP_PORT_MIN must be even so RTCP can use the following port")
	}

	present := []string{}
	for _, item := range []struct {
		name  string
		value string
	}{
		{EnvUniFiTalkSIPServer, c.UniFiTalkSIPServer},
		{EnvUniFiTalkSIPUsername, c.UniFiTalkSIPUsername},
		{EnvUniFiTalkSIPPassword, c.UniFiTalkSIPPassword},
		{EnvUniFiTalkSIPExtension, c.UniFiTalkSIPExtension},
	} {
		if item.value != "" {
			present = append(present, item.name)
		}
	}
	if len(present) > 0 && len(present) < 4 {
		problems = append(problems, "UniFi SIP registration requires UNIFI_TALK_SIP_SERVER, UNIFI_TALK_SIP_USERNAME, UNIFI_TALK_SIP_PASSWORD, and UNIFI_TALK_SIP_EXTENSION together")
	}
	return problems
}
