package prune

import (
	"fmt"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Dry          bool              `yaml:"dry"`
	Interval     Duration          `yaml:"interval"`
	IgnoredUsers []string          `yaml:"ignored_users"`
	Period       Duration          `yaml:"period"`
	Labels       map[string]string `yaml:"labels"`
}

// Duration is a custom type that can unmarshal Go duration strings from YAML
type Duration time.Duration

// UnmarshalYAML implements yaml.Unmarshaler for Duration
func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	var str string
	if err := node.Decode(&str); err != nil {
		return err
	}

	parsed, err := parseDuration(str)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", str, err)
	}

	*d = Duration(parsed)
	return nil
}

// parseDuration parses a duration string, handling additional units like 'd' for days
func parseDuration(s string) (time.Duration, error) {
	// Try the standard Go duration format first
	if d, err := time.ParseDuration(s); err == nil {
		return d, nil
	}

	// Handle days format by replacing 'd' with 'h' and multiplying by 24
	if len(s) > 1 && s[len(s)-1] == 'd' {
		num := s[:len(s)-1]
		if d, err := time.ParseDuration(num + "h"); err == nil {
			return d * 24, nil
		}
	}

	// Fallback to standard parsing to get the proper error
	return time.ParseDuration(s)
}

// ToDuration converts Duration to time.Duration
func (d Duration) ToDuration() time.Duration {
	return time.Duration(d)
}

// String returns the string representation of the duration
func (d Duration) String() string {
	return time.Duration(d).String()
}