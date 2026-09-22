package llm

import (
	"errors"
	"os"
	"strconv"
	"time"
)

const DefaultTimeout = 180 * time.Second
const DefaultMaxTokens = 8000

type Limits struct {
	Timeout   time.Duration
	MaxTokens int
}

func DefaultLimits() Limits {
	return Limits{Timeout: DefaultTimeout, MaxTokens: DefaultMaxTokens}
}

func LimitsFromEnv() (Limits, error) {
	limits := DefaultLimits()
	if value := os.Getenv("OPENROUTER_TIMEOUT"); value != "" {
		duration, err := time.ParseDuration(value)
		if err != nil {
			return Limits{}, errors.New("OPENROUTER_TIMEOUT must be a duration such as 180s")
		}
		limits.Timeout = duration
	}
	if value := os.Getenv("OPENROUTER_MAX_TOKENS"); value != "" {
		tokens, err := strconv.Atoi(value)
		if err != nil {
			return Limits{}, errors.New("OPENROUTER_MAX_TOKENS must be a positive integer")
		}
		limits.MaxTokens = tokens
	}
	return limits, limits.validate()
}

func (l Limits) validate() error {
	// Leave room for job and probe deadlines without overflowing time.Duration.
	if l.Timeout <= 0 || l.Timeout > time.Duration(1<<63-1)-time.Minute {
		return errors.New("OPENROUTER_TIMEOUT must be positive and leave room for a 60s deadline margin")
	}
	if l.MaxTokens <= 0 {
		return errors.New("OPENROUTER_MAX_TOKENS must be a positive integer")
	}
	return nil
}
